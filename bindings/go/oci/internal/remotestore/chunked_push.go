package remotestore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/errcode"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/oci/internal/introspection"
)

// DefaultChunkSize is the target PATCH chunk size when chunked upload is
// enabled without an explicit size (16 MiB).
const DefaultChunkSize int64 = 16 << 20

// DefaultChunkThreshold is the smallest blob eligible for chunked upload when
// no explicit threshold is configured (16 MiB): blobs below this go monolithic.
const DefaultChunkThreshold int64 = 16 << 20

// MaxChunkSize bounds the PATCH buffer the client will allocate. A registry can
// force the buffer size up via its advertised OCI-Chunk-Min-Length; an
// implausible value (up to math.MaxInt64) would otherwise panic make([]byte, n)
// or exhaust memory. Advertised minimums above this are rejected (128 MiB).
const MaxChunkSize int64 = 128 << 20

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
// the effective chunk threshold (max of ChunkThreshold and ChunkSize) are
// uploaded in chunks per the OCI Distribution Spec (POST session, PATCH chunks,
// PUT close); manifests, blobs that fit in a single chunk, and blobs below the
// threshold delegate to the embedded monolithic push. If chunking is disabled
// (ChunkSize <= 0) or the chunk protocol fails before any byte is consumed,
// Push falls back to the embedded monolithic push so no registry regresses.
//
// Reference: https://github.com/opencontainers/distribution-spec/blob/v1.1.1/spec.md#pushing-a-blob-in-chunks
func (r *RemoteStore) Push(ctx context.Context, expected ociImageSpecV1.Descriptor, content io.Reader) error {
	threshold := r.ChunkThreshold
	if threshold <= 0 {
		threshold = DefaultChunkThreshold
	}
	// Never chunk a blob that fits in a single chunk: a lone PATCH plus the
	// closing PUT is strictly worse than a monolithic POST/PUT. This holds even
	// when a caller configures ChunkThreshold below ChunkSize.
	if threshold < r.ChunkSize {
		threshold = r.ChunkSize
	}
	if r.ChunkSize <= 0 || expected.Size < threshold || introspection.IsOCICompliantManifest(expected) {
		return r.Repository.Push(ctx, expected, content)
	}
	_, err := r.pushChunked(ctx, expected.MediaType, expected.Digest, expected.Size, content, func() error {
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
	return r.pushChunked(ctx, partial.MediaType, partial.Digest, size, content, nil)
}

// chunkedUpload holds the mutable state of one chunked upload session: the
// current push location (updated after each PATCH), how many bytes have been
// streamed, whether any byte has been consumed (after which no monolithic
// fallback is possible), and the running digest of the streamed content.
type chunkedUpload struct {
	location *url.URL
	chunk    int64
	offset   int64
	consumed bool
	digester digest.Digester
}

// pushChunked runs the three-phase chunked upload protocol (open session,
// upload PATCH chunks, close with PUT) and returns the completed descriptor.
//
// When fallback is non-nil it is invoked for any failure that occurs before the
// first byte is consumed (so no registry regresses); once a chunk has been
// sent, an io.Reader cannot be rewound, so later failures return a wrapped
// error and best-effort cancel the session. When knownDigest is set it is
// verified against the streamed content and used to close the session; when
// empty the digest is computed from the stream.
func (r *RemoteStore) pushChunked(
	ctx context.Context,
	mediaType string,
	knownDigest digest.Digest,
	knownSize int64,
	content io.Reader,
	fallback func() error,
) (ociImageSpecV1.Descriptor, error) {
	ctx = auth.AppendRepositoryScope(ctx, r.Reference, auth.ActionPull, auth.ActionPush)

	up, err := r.openUploadSession(ctx, knownDigest)
	if err != nil {
		// The session never opened: no bytes consumed, fallback is always safe.
		return fallbackOrUnavailable(fallback, err)
	}

	if err := r.uploadChunks(ctx, up, content); err != nil {
		if !up.consumed {
			// Failed before the first PATCH; fall back to monolithic push.
			return fallbackOrUnavailable(fallback, err)
		}
		return ociImageSpecV1.Descriptor{}, err
	}

	finalDigest, err := resolveFinalDigest(up.digester.Digest(), knownDigest)
	if err != nil {
		r.cancelUpload(ctx, up.location)
		return ociImageSpecV1.Descriptor{}, err
	}

	if knownSize != blob.SizeUnknown && knownSize >= 0 && up.offset != knownSize {
		r.cancelUpload(ctx, up.location)
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("chunked blob push: uploaded %d bytes, expected %d", up.offset, knownSize)
	}

	if err := r.closeUploadSession(ctx, up, finalDigest); err != nil {
		return ociImageSpecV1.Descriptor{}, err
	}

	return ociImageSpecV1.Descriptor{
		MediaType: mediaType,
		Digest:    finalDigest,
		Size:      up.offset,
	}, nil
}

// fallbackOrUnavailable reports a pre-consumption failure: it runs the
// monolithic fallback when available, otherwise wraps ErrStreamingUnavailable
// so the caller can buffer and retry.
func fallbackOrUnavailable(fallback func() error, cause error) (ociImageSpecV1.Descriptor, error) {
	if fallback != nil {
		return ociImageSpecV1.Descriptor{}, fallback()
	}
	return ociImageSpecV1.Descriptor{}, fmt.Errorf("%w: %w", ErrStreamingUnavailable, cause)
}

// resolveFinalDigest picks the digest used to close the session: the computed
// digest when none was declared, or the declared digest after verifying it
// matches the streamed content (closing under a mismatching claim would push
// corrupt content under a false identifier).
func resolveFinalDigest(computed, known digest.Digest) (digest.Digest, error) {
	if known == "" {
		return computed, nil
	}
	if computed != known {
		return "", fmt.Errorf("chunked blob push: content digest %s does not match declared digest %s", computed, known)
	}
	return known, nil
}

// openUploadSession performs the POST that starts a blob upload and returns the
// initialised session state. Its digester matches knownDigest's algorithm (the
// canonical algorithm when no digest is declared), and a non-sha256 knownDigest
// additionally advertises that algorithm via the digest-algorithm query
// parameter. The returned chunkedUpload has its chunk size raised to any
// registry-advertised OCI-Chunk-Min-Length.
func (r *RemoteStore) openUploadSession(ctx context.Context, knownDigest digest.Digest) (*chunkedUpload, error) {
	uploads := r.endpoint(path.Join("/v2", r.Reference.Repository, "blobs", "uploads") + "/")
	// The running digest must use the declared algorithm; otherwise the
	// computed digest could never match knownDigest for non-sha256 blobs.
	algo := digest.Canonical
	if knownDigest != "" {
		algo = knownDigest.Algorithm()
		if !algo.Available() {
			return nil, fmt.Errorf("chunked blob push: digest algorithm %q is not available", algo)
		}
		if algo != digest.SHA256 {
			q := uploads.Query()
			q.Set("digest-algorithm", string(algo))
			uploads.RawQuery = q.Encode()
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploads.String(), http.NoBody)
	if err != nil {
		return nil, err
	}
	req.ContentLength = 0
	resp, err := r.do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		return nil, parseErrorResponse(resp)
	}
	location, err := resolveUploadLocation(resp, req)
	if err != nil {
		return nil, err
	}
	chunk := r.ChunkSize
	if minLen := parseChunkMinLength(resp); minLen > chunk {
		if minLen > MaxChunkSize {
			return nil, fmt.Errorf("chunked blob push: advertised OCI-Chunk-Min-Length %d exceeds the maximum supported chunk size %d", minLen, MaxChunkSize)
		}
		chunk = minLen
	}
	return &chunkedUpload{location: location, chunk: chunk, digester: algo.Digester()}, nil
}

// uploadChunks streams content in PATCH requests of up.chunk bytes, updating the
// running digest, byte offset, and push location as it goes. It sets
// up.consumed as soon as a read yields bytes; after that point the stream
// cannot be rewound, so failures cancel the session rather than falling back to
// the monolithic push.
func (r *RemoteStore) uploadChunks(ctx context.Context, up *chunkedUpload, content io.Reader) error {
	hasher := up.digester.Hash()
	buf := make([]byte, up.chunk)
	for {
		n, readErr := io.ReadFull(content, buf)
		if n > 0 {
			// Bytes have left the reader: the stream can no longer be rewound,
			// so no monolithic fallback is possible from here on. Mark consumed
			// before any error path so a mid-read failure is treated as
			// post-consumption rather than replayed.
			up.consumed = true
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil && readErr != io.ErrUnexpectedEOF {
			if up.consumed {
				r.cancelUpload(ctx, up.location)
			}
			return fmt.Errorf("chunked blob push: failed to read content after %d bytes: %w", up.offset, readErr)
		}
		if n == 0 {
			return nil
		}
		_, _ = hasher.Write(buf[:n])

		req, err := http.NewRequestWithContext(ctx, http.MethodPatch, up.location.String(), bytes.NewReader(buf[:n]))
		if err != nil {
			r.cancelUpload(ctx, up.location)
			return fmt.Errorf("chunked blob push: failed to build PATCH request: %w", err)
		}
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("Content-Range", fmt.Sprintf("%d-%d", up.offset, up.offset+int64(n)-1))
		req.ContentLength = int64(n)

		resp, err := r.do(req)
		if err != nil {
			return fmt.Errorf("chunked blob push: PATCH failed after %d bytes: %w", up.offset, err)
		}
		if resp.StatusCode != http.StatusAccepted {
			respErr := parseErrorResponse(resp)
			_ = resp.Body.Close()
			r.cancelUpload(ctx, up.location)
			return fmt.Errorf("chunked blob push: unexpected PATCH status %d after %d bytes: %w", resp.StatusCode, up.offset, respErr)
		}
		next, err := resolveUploadLocation(resp, req)
		_ = resp.Body.Close()
		if err != nil {
			r.cancelUpload(ctx, up.location)
			return fmt.Errorf("chunked blob push: invalid PATCH location after %d bytes: %w", up.offset, err)
		}
		up.location = next
		up.offset += int64(n)
	}
}

// closeUploadSession issues the final PUT that commits the blob under
// finalDigest and verifies the registry-reported digest when present.
func (r *RemoteStore) closeUploadSession(ctx context.Context, up *chunkedUpload, finalDigest digest.Digest) error {
	q := up.location.Query()
	q.Set("digest", finalDigest.String())
	up.location.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, up.location.String(), http.NoBody)
	if err != nil {
		r.cancelUpload(ctx, up.location)
		return fmt.Errorf("chunked blob push: failed to build PUT request: %w", err)
	}
	req.ContentLength = 0
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := r.do(req)
	if err != nil {
		return fmt.Errorf("chunked blob push: PUT failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("chunked blob push: unexpected PUT status %d: %w", resp.StatusCode, parseErrorResponse(resp))
	}
	if returned := resp.Header.Get("Docker-Content-Digest"); returned != "" && returned != finalDigest.String() {
		return fmt.Errorf("chunked blob push: registry returned digest %q, expected %q", returned, finalDigest.String())
	}
	return nil
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
