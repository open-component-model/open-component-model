package remotestore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/errcode"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/oci/internal/introspection"
	"ocm.software/open-component-model/bindings/go/oci/spec"
)

// ErrTagDeletionDisabled is returned when the registry responds with 405 Method Not Allowed
// to a tag-deletion request, indicating that the registry does not support tag deletion
// (e.g. REGISTRY_STORAGE_DELETE_ENABLED is not set).
var ErrTagDeletionDisabled = fmt.Errorf("registry does not support tag deletion (405 Method Not Allowed)")

// DefaultChunkSize is the target PATCH chunk size when chunked upload is
// enabled without an explicit size (16 MiB).
const DefaultChunkSize int64 = 16 << 20

// DefaultChunkThreshold is the smallest blob eligible for chunked upload when
// no explicit threshold is configured (16 MiB): blobs below this go monolithic.
const DefaultChunkThreshold int64 = 16 << 20

// RemoteStore wraps *remote.Repository and adds content.Untagger support plus
// chunked blob upload (see Push).
//
// oras-go implements content.Untagger only for its local OCI layout store
// (content/oci.Store). The remote registry client (registry/remote.Repository)
// intentionally omits it: the OCI Distribution Spec treats
// DELETE /v2/<name>/manifests/<tag> as optional, and not all registries honor it.
//
// oras-go's remote.Repository also pushes blobs monolithically only; RemoteStore
// overrides Push to optionally upload large blobs in chunks.
type RemoteStore struct {
	*remote.Repository

	// ChunkSize is the target size in bytes for each PATCH chunk. If <= 0,
	// chunked upload is disabled and Push always delegates to the embedded
	// monolithic push. The registry-advertised OCI-Chunk-Min-Length (from the
	// session POST response) raises this floor for all but the final chunk.
	ChunkSize int64

	// ChunkThreshold is the minimum blob size in bytes for chunked upload to
	// engage. Blobs of Size < ChunkThreshold (and all manifests) use the
	// embedded monolithic Push. If <= 0, DefaultChunkThreshold is used.
	ChunkThreshold int64
}

var (
	_ spec.Store       = (*RemoteStore)(nil) // general store spec
	_ content.Untagger = (*RemoteStore)(nil) // content.Untagger opt-in
)

// Untag removes the given tag from the remote registry without deleting the underlying manifest.
// The registry must have tag deletion enabled; a 405 response means it is disabled server-side.
func (r *RemoteStore) Untag(ctx context.Context, reference string) error {
	ref := r.Reference
	ref.Reference = reference
	if err := ref.ValidateReferenceAsTag(); err != nil {
		return fmt.Errorf("invalid tag reference %q: %w", reference, err)
	}
	ctx = auth.AppendRepositoryScope(ctx, ref, auth.ActionDelete)

	scheme := "https"
	if r.PlainHTTP {
		scheme = "http"
	}
	endpoint := &url.URL{
		Scheme: scheme,
		Host:   ref.Host(),
		Path:   path.Join("/v2", ref.Repository, "manifests", reference),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint.String(), http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to build delete request for alias %q: %w", reference, err)
	}

	client := r.Client
	if client == nil {
		client = auth.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete alias %q: %w", reference, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Error("Failed to close response body for alias", "reference", reference, "err", err)
		}
	}()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusNoContent:
		return nil
	case http.StatusNotFound:
		return errdef.ErrNotFound
	case http.StatusMethodNotAllowed:
		return ErrTagDeletionDisabled
	default:
		errResp := &errcode.ErrorResponse{
			Method:     resp.Request.Method,
			URL:        resp.Request.URL,
			StatusCode: resp.StatusCode,
		}
		var body struct {
			Errors errcode.Errors `json:"errors"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
			errResp.Errors = body.Errors
		}
		return errResp
	}
}

// ErrStreamingUnavailable is returned by PushStreaming when chunked upload is
// disabled (ChunkSize <= 0) or when the session cannot be established before
// any content is consumed. In streaming mode there is no monolithic fallback
// (the digest is unknown up front), so the caller must buffer and retry via
// the regular Push path.
var ErrStreamingUnavailable = errors.New("streaming chunked upload unavailable")

// StreamingPusher is implemented by stores that can upload a blob without a
// precomputed digest, streaming the content and computing the digest during
// upload. Callers use this to avoid buffering blobs of unknown size/digest.
type StreamingPusher interface {
	// PushStreaming uploads content described only by partial (MediaType is
	// used; Digest/Size are ignored and computed during upload) and returns the
	// completed descriptor with the computed Digest and Size. If streaming is
	// unavailable it returns an error wrapping ErrStreamingUnavailable and no
	// content is consumed, so the caller may safely buffer and fall back.
	PushStreaming(ctx context.Context, partial ociImageSpecV1.Descriptor, content io.Reader) (ociImageSpecV1.Descriptor, error)
}

var _ StreamingPusher = (*RemoteStore)(nil)

// Push pushes the content matching the expected descriptor. Blobs at or above
// the configured chunk threshold are uploaded in chunks per the OCI
// Distribution Spec (POST session, PATCH chunks, PUT close); manifests and
// small blobs delegate to the embedded monolithic push. If chunking is
// disabled (ChunkSize <= 0) or the chunk protocol fails before any byte is
// consumed, Push falls back to the embedded monolithic push so no registry
// regresses.
//
// Reference: https://github.com/opencontainers/distribution-spec/blob/v1.1.1/spec.md#pushing-a-blob-in-chunks
func (r *RemoteStore) Push(ctx context.Context, expected ociImageSpecV1.Descriptor, content io.Reader) error {
	threshold := r.ChunkThreshold
	if threshold <= 0 {
		threshold = DefaultChunkThreshold
	}
	if r.ChunkSize <= 0 || expected.Size < threshold || introspection.IsOCICompliantManifest(expected) {
		return r.Repository.Push(ctx, expected, content)
	}
	_, err := r.pushChunkedStream(ctx, expected.MediaType, expected.Digest, expected.Size, content, func() error {
		return r.Repository.Push(ctx, expected, content)
	})
	return err
}

// PushStreaming implements StreamingPusher: it uploads content without needing
// its size in advance, computing the digest and size during the chunked upload
// and returning the completed descriptor. The MediaType is taken from partial.
//
// If partial.Digest is set it is treated as the expected digest: the streamed
// content is verified against it and the session is closed with it. If it is
// empty the digest is computed from the streamed bytes. partial.Size, when
// non-negative, is verified against the number of bytes streamed.
//
// Unlike Push there is no monolithic fallback, because a monolithic upload
// requires the size (Content-Length) up front. If chunking is disabled or the
// session cannot be opened before any byte is consumed, it returns an error
// wrapping ErrStreamingUnavailable without consuming content, so the caller can
// buffer and retry via Push.
func (r *RemoteStore) PushStreaming(ctx context.Context, partial ociImageSpecV1.Descriptor, content io.Reader) (ociImageSpecV1.Descriptor, error) {
	if r.ChunkSize <= 0 {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("chunk size not configured: %w", ErrStreamingUnavailable)
	}
	size := partial.Size
	if size == 0 {
		// A zero Size on an incomplete descriptor is treated as unknown; an empty
		// blob is uploaded as a single empty chunk and verified by digest.
		size = blob.SizeUnknown
	}
	return r.pushChunkedStream(ctx, partial.MediaType, partial.Digest, size, content, nil)
}

// pushChunkedStream performs the chunked upload protocol.
//
// When knownDigest is non-empty the upload closes with that digest and, on a
// pre-consumption failure (session POST fails, or the first PATCH not yet
// sent), it invokes fallback (the embedded monolithic push) so no registry
// regresses. When knownDigest is empty the digest is computed from the streamed
// bytes and used to close the session; in that mode there is no fallback and a
// pre-consumption session failure returns an error wrapping
// ErrStreamingUnavailable. Post-consumption failures always return a wrapped
// error and attempt a best-effort session cancel (DELETE), since an io.Reader
// cannot be rewound once read.
//
// It returns the completed descriptor (MediaType, computed-or-known Digest, and
// the number of bytes uploaded as Size).
func (r *RemoteStore) pushChunkedStream(
	ctx context.Context,
	mediaType string,
	knownDigest digest.Digest,
	knownSize int64,
	content io.Reader,
	fallback func() error,
) (ociImageSpecV1.Descriptor, error) {
	ctx = auth.AppendRepositoryScope(ctx, r.Reference, auth.ActionPull, auth.ActionPush)

	// preConsumptionFailure decides how to report a failure that happens before
	// any byte of content has been consumed: fall back to monolithic push when a
	// known digest and fallback are available, otherwise signal that streaming
	// is unavailable so the caller can buffer and retry.
	preConsumptionFailure := func(cause error) (ociImageSpecV1.Descriptor, error) {
		if fallback != nil {
			return ociImageSpecV1.Descriptor{}, fallback()
		}
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("%w: %w", ErrStreamingUnavailable, cause)
	}

	// 1. Open the upload session.
	uploads := r.endpoint(path.Join("/v2", r.Reference.Repository, "blobs", "uploads") + "/")
	if knownDigest != "" {
		if algo := knownDigest.Algorithm(); algo != digest.SHA256 {
			q := uploads.Query()
			q.Set("digest-algorithm", string(algo))
			uploads.RawQuery = q.Encode()
		}
	}
	postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, uploads.String(), http.NoBody)
	if err != nil {
		return preConsumptionFailure(err)
	}
	postReq.ContentLength = 0
	postResp, err := r.do(postReq)
	if err != nil {
		return preConsumptionFailure(err)
	}
	if postResp.StatusCode != http.StatusAccepted {
		respErr := parseErrorResponse(postResp)
		_ = postResp.Body.Close()
		return preConsumptionFailure(respErr)
	}
	location, err := resolveUploadLocation(postResp, postReq)
	if err != nil {
		_ = postResp.Body.Close()
		return preConsumptionFailure(err)
	}
	chunk := r.ChunkSize
	if minLen := parseChunkMinLength(postResp); minLen > chunk {
		chunk = minLen
	}
	_ = postResp.Body.Close()

	// Always compute the streamed digest: when knownDigest is empty it becomes
	// the blob digest; when it is set the computed value is verified against it
	// so a lazily-streamed blob that claims a digest cannot upload corrupt data.
	digester := digest.Canonical.Digester()

	// 2. Upload chunks via PATCH. Fallback stays available until the first
	// PATCH is issued (no bytes consumed yet).
	var offset int64
	buf := make([]byte, chunk)
	consumed := false
	for {
		n, readErr := io.ReadFull(content, buf)
		if readErr == io.EOF {
			break
		}
		if readErr != nil && readErr != io.ErrUnexpectedEOF {
			if !consumed {
				return preConsumptionFailure(readErr)
			}
			r.cancelUpload(ctx, location)
			return ociImageSpecV1.Descriptor{}, fmt.Errorf("chunked blob push: failed to read content after %d bytes: %w", offset, readErr)
		}
		if n == 0 {
			break
		}
		_, _ = digester.Hash().Write(buf[:n])
		patchReq, err := http.NewRequestWithContext(ctx, http.MethodPatch, location.String(), bytes.NewReader(buf[:n]))
		if err != nil {
			if !consumed {
				return preConsumptionFailure(err)
			}
			r.cancelUpload(ctx, location)
			return ociImageSpecV1.Descriptor{}, fmt.Errorf("chunked blob push: failed to build PATCH request: %w", err)
		}
		patchReq.Header.Set("Content-Type", "application/octet-stream")
		patchReq.Header.Set("Content-Range", fmt.Sprintf("%d-%d", offset, offset+int64(n)-1))
		patchReq.ContentLength = int64(n)
		consumed = true
		patchResp, err := r.do(patchReq)
		if err != nil {
			return ociImageSpecV1.Descriptor{}, fmt.Errorf("chunked blob push: PATCH failed after %d bytes: %w", offset, err)
		}
		if patchResp.StatusCode != http.StatusAccepted {
			respErr := parseErrorResponse(patchResp)
			_ = patchResp.Body.Close()
			r.cancelUpload(ctx, location)
			return ociImageSpecV1.Descriptor{}, fmt.Errorf("chunked blob push: unexpected PATCH status %d after %d bytes: %w", patchResp.StatusCode, offset, respErr)
		}
		next, err := resolveUploadLocation(patchResp, patchReq)
		_ = patchResp.Body.Close()
		if err != nil {
			r.cancelUpload(ctx, location)
			return ociImageSpecV1.Descriptor{}, fmt.Errorf("chunked blob push: invalid PATCH location after %d bytes: %w", offset, err)
		}
		location = next
		offset += int64(n)
	}

	computed := digester.Digest()
	finalDigest := computed
	if knownDigest != "" {
		// A caller-supplied digest must match what we actually streamed; closing
		// the session with the claimed digest over mismatching bytes would push
		// corrupt content under a false identifier.
		if computed != knownDigest {
			r.cancelUpload(ctx, location)
			return ociImageSpecV1.Descriptor{}, fmt.Errorf("chunked blob push: content digest %s does not match declared digest %s", computed, knownDigest)
		}
		finalDigest = knownDigest
	}

	// If a size was declared up front, guard against a truncated stream.
	if knownSize != blob.SizeUnknown && knownSize >= 0 && offset != knownSize {
		r.cancelUpload(ctx, location)
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("chunked blob push: uploaded %d bytes, expected %d", offset, knownSize)
	}

	// 3. Close the session with the whole-blob digest.
	q := location.Query()
	q.Set("digest", finalDigest.String())
	location.RawQuery = q.Encode()
	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, location.String(), http.NoBody)
	if err != nil {
		if !consumed {
			return preConsumptionFailure(err)
		}
		r.cancelUpload(ctx, location)
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("chunked blob push: failed to build PUT request: %w", err)
	}
	putReq.ContentLength = 0
	putReq.Header.Set("Content-Type", "application/octet-stream")
	putResp, err := r.do(putReq)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("chunked blob push: PUT failed: %w", err)
	}
	defer func() {
		if cerr := putResp.Body.Close(); cerr != nil {
			slog.Error("failed to close chunked blob push PUT response body", "err", cerr)
		}
	}()
	if putResp.StatusCode != http.StatusCreated {
		respErr := parseErrorResponse(putResp)
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("chunked blob push: unexpected PUT status %d: %w", putResp.StatusCode, respErr)
	}
	if returned := putResp.Header.Get("Docker-Content-Digest"); returned != "" && returned != finalDigest.String() {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("chunked blob push: registry returned digest %q, expected %q", returned, finalDigest.String())
	}
	return ociImageSpecV1.Descriptor{
		MediaType: mediaType,
		Digest:    finalDigest,
		Size:      offset,
	}, nil
}

// do sends an HTTP request using the same client resolution and response
// handling as the embedded oras *remote.Repository: it uses r.Client (falling
// back to auth.DefaultClient) so retries, authentication, TLS, proxy and header
// configuration are respected identically, and it applies r.HandleWarning to
// response Warning headers just as oras' internal do() does.
func (r *RemoteStore) do(req *http.Request) (*http.Response, error) {
	client := r.Client
	if client == nil {
		client = auth.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if r.HandleWarning != nil {
		for _, h := range resp.Header.Values("Warning") {
			if value, perr := parseWarningHeader(h); perr == nil {
				r.HandleWarning(remote.Warning{WarningValue: value})
			}
		}
	}
	return resp, nil
}

// parseWarningHeader parses a distribution-spec Warning header value (a 299
// warn-code with quoted warn-text) into a remote.WarningValue. It mirrors the
// unexported parser in oras-go so chunked push handles warnings identically.
// Warnings in any other format are rejected.
func parseWarningHeader(header string) (remote.WarningValue, error) {
	if len(header) < 9 || !strings.HasPrefix(header, `299 - "`) || !strings.HasSuffix(header, `"`) {
		return remote.WarningValue{}, fmt.Errorf("unexpected warning format: %s", header)
	}
	text, err := strconv.Unquote(header[6:]) // behind `299 - `, quoted by "
	if err != nil {
		return remote.WarningValue{}, fmt.Errorf("unexpected warning text: %s: %w", header, err)
	}
	return remote.WarningValue{Code: 299, Agent: "-", Text: text}, nil
}

// endpoint builds an absolute registry URL for the given path using the
// repository's scheme and host.
func (r *RemoteStore) endpoint(p string) *url.URL {
	scheme := "https"
	if r.PlainHTTP {
		scheme = "http"
	}
	return &url.URL{Scheme: scheme, Host: r.Reference.Host(), Path: p}
}

// cancelUpload issues a best-effort DELETE to release an in-progress upload
// session. Per the OCI spec, clients SHOULD ignore any failures.
func (r *RemoteStore) cancelUpload(ctx context.Context, location *url.URL) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, location.String(), http.NoBody)
	if err != nil {
		return
	}
	resp, err := r.do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

// resolveUploadLocation extracts the next upload Location from a response,
// resolving it against the request URL and rejecting cross-host redirects and
// scheme downgrades that could leak credentials to an attacker-controlled host.
func resolveUploadLocation(resp *http.Response, req *http.Request) (*url.URL, error) {
	location, err := resp.Location()
	if err != nil {
		return nil, err
	}
	// Work around registries that drop an explicit :443 from the Location host
	// (see oras-go issue 177): if the request used :443 and the location omits
	// it on the same hostname, add it back.
	if req.URL.Port() == "443" && location.Hostname() == req.URL.Hostname() && location.Port() == "" {
		location.Host = location.Hostname() + ":443"
	}
	if !sameUploadHost(location, req.URL) {
		return nil, fmt.Errorf("upload Location %q is on a different host than the registry %q", location.Host, req.URL.Host)
	}
	if req.URL.Scheme == "https" && location.Scheme != "https" {
		return nil, fmt.Errorf("upload Location %q downgrades scheme from https", location.Host)
	}
	return location, nil
}

// sameUploadHost reports whether location and reqURL refer to the same host,
// normalizing implicit default ports (80 for http, 443 for https) so that e.g.
// "example.com" and "example.com:443" compare equal over HTTPS.
func sameUploadHost(location, reqURL *url.URL) bool {
	if location.Hostname() != reqURL.Hostname() {
		return false
	}
	canonicalPort := func(u *url.URL) string {
		if p := u.Port(); p != "" {
			return p
		}
		if u.Scheme == "https" {
			return "443"
		}
		return "80"
	}
	return canonicalPort(location) == canonicalPort(reqURL)
}

// parseChunkMinLength reads the registry-advertised OCI-Chunk-Min-Length header
// from a session response, returning 0 when absent or unparsable. The header is
// read via its canonical MIME form (Get canonicalizes the key regardless).
func parseChunkMinLength(resp *http.Response) int64 {
	v := resp.Header.Get("Oci-Chunk-Min-Length")
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// parseErrorResponse decodes a registry error response body into an
// errcode.ErrorResponse for wrapped error reporting.
func parseErrorResponse(resp *http.Response) error {
	errResp := &errcode.ErrorResponse{
		Method:     resp.Request.Method,
		URL:        resp.Request.URL,
		StatusCode: resp.StatusCode,
	}
	var body struct {
		Errors errcode.Errors `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
		errResp.Errors = body.Errors
	}
	return errResp
}
