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
	"time"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	slogcontext "github.com/veqryn/slog-context"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/errcode"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/oci/internal/introspection"
)

// DefaultChunkSize is the target PATCH chunk size when none is configured
// (4 MiB). It is the largest chunk ghcr.io accepts; larger chunks are answered
// with 416 there.
const DefaultChunkSize int64 = 4 << 20

// DefaultChunkThreshold is the smallest blob with a known digest and size that
// Push uploads in chunks when no explicit threshold is configured (512 MiB).
// Below it a single request is faster and works on every registry; from it on,
// bounded requests that are retried individually pay off. Blobs of unknown size
// or digest are always chunked (see PushStreaming).
const DefaultChunkThreshold int64 = 512 << 20

// MinRetryChunkSize is the smallest chunk a rejected PATCH is retried with
// (256 KiB). Halving stops here (or at the registry's OCI-Chunk-Min-Length)
// and the upload fails instead.
const MinRetryChunkSize int64 = 256 << 10

// MaxChunkSize bounds the PATCH buffer the client will allocate (128 MiB),
// which is sized to the effective chunk length: a configured ChunkSize above
// it is capped to it, and an upload whose registry advertises a larger
// OCI-Chunk-Min-Length is refused (Push then falls back to a single request).
// Without this bound an implausible value (up to math.MaxInt64) would panic
// make([]byte, n) or exhaust memory.
const MaxChunkSize int64 = 128 << 20

// StreamingPusher is implemented by stores that can upload a blob whose digest
// and/or size is not known before the upload starts, computing (or verifying)
// both from the streamed bytes. The regular Push also streams its content to
// the registry, but only for a blob whose digest and size are already known;
// PushStreaming exists for the case where they are not, so the content need not
// be buffered up front just to precompute the descriptor.
type StreamingPusher interface {
	// PushStreaming uploads content described only by partial. Its MediaType is
	// carried onto the returned descriptor. A non-empty partial.Digest and a
	// non-negative, non-zero partial.Size are treated as expected values and
	// verified against the streamed bytes; when either is absent it is computed
	// during upload. The returned descriptor carries the final Digest and Size.
	PushStreaming(ctx context.Context, partial ociImageSpecV1.Descriptor, content io.Reader) (ociImageSpecV1.Descriptor, error)
}

var _ StreamingPusher = (*RemoteStore)(nil)

// Push pushes the content matching the expected descriptor. Blobs of at least
// the chunk threshold (see RemoteStore.ChunkThreshold) that do not fit in a
// single chunk are uploaded in chunks per the OCI Distribution Spec (POST
// session, PATCH chunks, PUT close), so every request stays bounded and is
// retried on its own; smaller blobs and all manifests use the embedded
// monolithic push.
//
// If the registry refuses the chunked upload before accepting any byte of it,
// Push logs a warning and pushes the blob monolithically instead, replaying the
// chunk already read ahead of the unread content, so at most one chunk is held
// in memory. Once the registry accepted a chunk the content cannot be rewound,
// so a later failure is returned.
//
// Reference: https://github.com/opencontainers/distribution-spec/blob/v1.1.1/spec.md#pushing-a-blob-in-chunks
func (r *RemoteStore) Push(ctx context.Context, expected ociImageSpecV1.Descriptor, content io.Reader) error {
	if !r.chunked(expected) {
		return r.Repository.Push(ctx, expected, content)
	}
	_, err := r.pushChunked(ctx, expected.MediaType, expected.Digest, expected.Size, content)
	var refused *refusedError
	if !errors.As(err, &refused) {
		return err
	}
	if ctx.Err() != nil {
		return refused.err
	}
	slogcontext.FromCtx(ctx).Log(ctx, slog.LevelWarn, "registry refused chunked blob upload, retrying as monolithic upload",
		slog.String("repository", r.Reference.String()), slog.Any("error", refused.err))
	body := content
	if len(refused.read) > 0 {
		body = io.MultiReader(bytes.NewReader(refused.read), content)
	}
	if err := r.Repository.Push(ctx, expected, body); err != nil {
		return fmt.Errorf("monolithic push after refused chunked upload failed: %w (chunked upload: %w)", err, refused.err)
	}
	return nil
}

// chunked reports whether Push uploads desc in chunks: a blob of at least the
// chunk threshold (ChunkThreshold, or DefaultChunkThreshold when unset) that
// does not fit in a single chunk. Manifests are never chunked; they are pushed
// to the manifests endpoint.
func (r *RemoteStore) chunked(desc ociImageSpecV1.Descriptor) bool {
	threshold := r.ChunkThreshold
	if threshold <= 0 {
		threshold = DefaultChunkThreshold
	}
	return desc.Size >= threshold && desc.Size > r.chunkSize() && !introspection.IsOCICompliantManifest(desc)
}

// refusedError reports that the registry refused a chunked upload before
// accepting any byte of it. read holds the content already consumed from the
// source for that upload (at most one chunk), so Push can still upload the blob
// monolithically by replaying it ahead of the unread content.
type refusedError struct {
	read []byte
	err  error
}

func (e *refusedError) Error() string { return e.err.Error() }
func (e *refusedError) Unwrap() error { return e.err }

// PushStreaming implements StreamingPusher. A complete descriptor (digest and
// size known) is pushed like Push: in one request below the chunk threshold, in
// chunks from it on. Otherwise content is uploaded in chunks per the OCI
// Distribution Spec (POST session, PATCH chunks, PUT close), computing the
// digest and size during the upload. The MediaType is taken from partial.
//
// If partial.Digest is set it is treated as the expected digest: the streamed
// content is verified against it and the session is closed with it. If it is
// empty the digest is computed from the streamed bytes. partial.Size, when
// non-negative, is verified against the number of bytes streamed.
//
// A chunk the registry rejects as too large is retried in smaller pieces on
// the same session; at most one chunk of content is held in memory. Any other
// failure is returned: without a complete descriptor there is no monolithic
// fallback, and the blob is never buffered to build one.
//
// Reference: https://github.com/opencontainers/distribution-spec/blob/v1.1.1/spec.md#pushing-a-blob-in-chunks
func (r *RemoteStore) PushStreaming(ctx context.Context, partial ociImageSpecV1.Descriptor, content io.Reader) (ociImageSpecV1.Descriptor, error) {
	if partial.Digest != "" && partial.Size > 0 {
		if err := r.Push(ctx, partial, content); err != nil {
			return ociImageSpecV1.Descriptor{}, err
		}
		return partial, nil
	}
	size := partial.Size
	if size == 0 {
		// A zero Size on an incomplete descriptor is treated as unknown; an empty
		// blob is uploaded as a single empty chunk and verified by digest.
		size = blob.SizeUnknown
	}
	desc, err := r.pushChunked(ctx, partial.MediaType, partial.Digest, size, content)
	// Without a complete descriptor there is no fallback; drop the chunk a
	// refusal carries for one.
	var refused *refusedError
	if errors.As(err, &refused) {
		return ociImageSpecV1.Descriptor{}, refused.err
	}
	return desc, err
}

// chunkedUpload holds the mutable state of one chunked upload session: the
// current push location (updated after each PATCH), the current chunk size
// (lowered when the registry rejects a chunk as too large), the smallest chunk
// size the registry allows, how many bytes the registry has accepted, and the
// running digest of the streamed content.
type chunkedUpload struct {
	location *url.URL
	chunk    int64
	minChunk int64
	offset   int64
	digester digest.Digester
}

// shrink halves the chunk size, rounding up, after the registry rejected a
// chunk of rejected bytes as too large; rounding up avoids a stray short PATCH
// in the middle of the upload. It reports false when the halved size would drop
// below MinRetryChunkSize or the registry's OCI-Chunk-Min-Length.
func (up *chunkedUpload) shrink(rejected int64) bool {
	next := (rejected + 1) / 2
	if next < max(MinRetryChunkSize, up.minChunk) {
		return false
	}
	up.chunk = next
	return true
}

// pushChunked runs the three-phase chunked upload protocol (open session,
// upload PATCH chunks, close with PUT) and returns the completed descriptor.
//
// A chunk the registry rejects as too large is retried in smaller pieces on the
// same session (see patchChunk). If the registry refuses the upload before
// accepting any byte (the session cannot be opened, or the first chunk is
// rejected), the error is a *refusedError. Once the session is open, every
// failure, including network errors and a cancelled ctx, cancels it (see
// cancelUpload) and is returned wrapped. Content is never buffered beyond one
// chunk. When knownDigest is set it is verified against the streamed content
// and used to close the session; when empty the digest is computed from the
// stream.
func (r *RemoteStore) pushChunked(
	ctx context.Context,
	mediaType string,
	knownDigest digest.Digest,
	knownSize int64,
	content io.Reader,
) (_ ociImageSpecV1.Descriptor, err error) {
	ctx = auth.AppendRepositoryScope(ctx, r.Reference, auth.ActionPull, auth.ActionPush)

	up, err := r.openUploadSession(ctx, knownDigest)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, &refusedError{err: fmt.Errorf("chunked blob push: failed to open upload session: %w", err)}
	}
	defer func() {
		if err != nil {
			r.cancelUpload(ctx, up.location)
		}
	}()

	if err = r.uploadChunks(ctx, up, content, knownSize); err != nil {
		return ociImageSpecV1.Descriptor{}, err
	}

	finalDigest, err := resolveFinalDigest(up.digester.Digest(), knownDigest)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, err
	}

	if knownSize != blob.SizeUnknown && knownSize >= 0 && up.offset != knownSize {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("chunked blob push: uploaded %d bytes, expected %d", up.offset, knownSize)
	}

	if err = r.closeUploadSession(ctx, up, finalDigest); err != nil {
		return ociImageSpecV1.Descriptor{}, err
	}

	return ociImageSpecV1.Descriptor{
		MediaType: mediaType,
		Digest:    finalDigest,
		Size:      up.offset,
	}, nil
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
// parameter. The returned chunkedUpload has its chunk size (see chunkSize)
// raised to any registry-advertised OCI-Chunk-Min-Length. An advertised
// minimum above MaxChunkSize is refused here, releasing the session, before
// any content is consumed.
func (r *RemoteStore) openUploadSession(ctx context.Context, knownDigest digest.Digest) (*chunkedUpload, error) {
	chunk := r.chunkSize()
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
	minLen := parseChunkMinLength(resp)
	if minLen > chunk {
		if minLen > MaxChunkSize {
			r.cancelUpload(ctx, location)
			return nil, fmt.Errorf("advertised OCI-Chunk-Min-Length %d exceeds the maximum supported chunk size %d", minLen, MaxChunkSize)
		}
		chunk = minLen
	}
	return &chunkedUpload{location: location, chunk: chunk, minChunk: minLen, digester: algo.Digester()}, nil
}

// chunkSize returns the configured ChunkSize, or DefaultChunkSize when it is
// not set (<= 0), capped at MaxChunkSize.
func (r *RemoteStore) chunkSize() int64 {
	if r.ChunkSize > 0 {
		return min(r.ChunkSize, MaxChunkSize)
	}
	return DefaultChunkSize
}

// uploadChunks streams content in PATCH requests of up.chunk bytes, updating the
// running digest, byte offset, and push location as it goes. The buffer is
// allocated once at the initial chunk size; after a shrink only its prefix is
// used.
func (r *RemoteStore) uploadChunks(ctx context.Context, up *chunkedUpload, content io.Reader, knownSize int64) error {
	hasher := up.digester.Hash()
	bufSize := up.chunk
	if knownSize >= 0 && knownSize < bufSize {
		bufSize = knownSize
	}
	buf := make([]byte, bufSize)
	for {
		n, readErr := io.ReadFull(content, buf[:min(int64(len(buf)), up.chunk)])
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil && readErr != io.ErrUnexpectedEOF {
			return fmt.Errorf("chunked blob push: failed to read content after %d bytes: %w", up.offset, readErr)
		}
		if n == 0 {
			return nil
		}
		_, _ = hasher.Write(buf[:n])
		if err := r.patchChunk(ctx, up, buf[:n]); err != nil {
			return err
		}
	}
}

// patchChunk uploads data at up.offset. A registry that rejects the request
// body as too large (413, or 416 as ghcr.io answers above its 4 MiB PATCH
// limit) keeps the session at the last accepted offset, so data is resent on
// the same session in pieces of half the rejected size, and the smaller size
// is kept for the rest of the upload. Only data, already held in the chunk
// buffer, is retried; the reader is never rewound. A failure before the
// registry accepted any byte of the session is returned as a *refusedError
// carrying data, which is then all content read so far.
func (r *RemoteStore) patchChunk(ctx context.Context, up *chunkedUpload, data []byte) error {
	for len(data) > 0 {
		part := data[:min(int64(len(data)), up.chunk)]
		status, err := r.sendPatch(ctx, up, part)
		if err == nil {
			data = data[len(part):]
			continue
		}
		if (status == http.StatusRequestEntityTooLarge || status == http.StatusRequestedRangeNotSatisfiable) && up.shrink(int64(len(part))) {
			slogcontext.FromCtx(ctx).Log(ctx, slog.LevelWarn, "registry rejected blob chunk, retrying with a smaller chunk size",
				slog.String("repository", r.Reference.String()), slog.Int("rejectedSize", len(part)), slog.Int64("chunkSize", up.chunk), slog.Any("error", err))
			continue
		}
		if up.offset == 0 && (status < 200 || status > 299) {
			return &refusedError{read: data, err: err}
		}
		return err
	}
	return nil
}

// sendPatch issues one PATCH carrying part at up.offset and, on any 2xx,
// advances the offset and push location. The spec requires 202, but ECR
// answers 201; the offset is tracked from what was sent, not from the
// response's Range header. It returns the response status (0 when no
// response was received) with any error.
func (r *RemoteStore) sendPatch(ctx context.Context, up *chunkedUpload, part []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, up.location.String(), bytes.NewReader(part))
	if err != nil {
		return 0, fmt.Errorf("chunked blob push: failed to build PATCH request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Range", fmt.Sprintf("%d-%d", up.offset, up.offset+int64(len(part))-1))
	req.ContentLength = int64(len(part))

	resp, err := r.do(req)
	if err != nil {
		return 0, fmt.Errorf("chunked blob push: PATCH failed after %d bytes: %w", up.offset, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, fmt.Errorf("chunked blob push: unexpected PATCH status %d after %d bytes: %w", resp.StatusCode, up.offset, parseErrorResponse(resp))
	}
	next, err := resolveUploadLocation(resp, req)
	if err != nil {
		return resp.StatusCode, fmt.Errorf("chunked blob push: invalid PATCH location after %d bytes: %w", up.offset, err)
	}
	up.location = next
	up.offset += int64(len(part))
	return resp.StatusCode, nil
}

// closeUploadSession issues the final PUT that commits the blob under
// finalDigest and verifies the registry-reported digest when present. The
// digest query goes on a copy of the push location, so a failed close cancels
// the session at its plain location.
func (r *RemoteStore) closeUploadSession(ctx context.Context, up *chunkedUpload, finalDigest digest.Digest) error {
	putURL := *up.location
	q := putURL.Query()
	q.Set("digest", finalDigest.String())
	putURL.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, putURL.String(), http.NoBody)
	if err != nil {
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
// It also refuses to auto-follow redirects for body-carrying requests: a
// registry-controlled 307/308 must never replay the blob body to a host this
// package has not validated (SSRF, CWE-918). Rather than clear Request.GetBody
// (which oras' auth.Client requires to rewind the body for its own same-origin
// 401 retry, see auth.Client.Do -> rewindRequestBody), it runs the request
// through a client whose CheckRedirect stops at the first response. oras'
// send() preserves a caller-set CheckRedirect, so the auth retry still works
// while the standard library returns the 3xx instead of following it; the
// upload's own status checks then reject it and no body leaks. Legitimate
// upload-location handoffs still occur explicitly through the Location header,
// which resolveUploadLocation validates for host and scheme.
func (r *RemoteStore) do(req *http.Request) (*http.Response, error) {
	client := r.Client
	if client == nil {
		client = auth.DefaultClient
	}
	if req.Body != nil && req.Body != http.NoBody {
		client = noFollowRedirects(client)
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

// noFollowRedirects returns a client equivalent to c but that stops at the
// first response instead of following redirects, without mutating c. It shares
// c's credential cache and configuration; only redirect handling changes.
//
// The redirect policy lives on a concrete *http.Client, reached differently per
// implementation: an *auth.Client carries it on its inner client (oras' send()
// copies that client and preserves a caller-set CheckRedirect, so the auth
// challenge/rewind flow is untouched), and a plain *http.Client carries it
// directly. Any other remote.Client keeps its redirect policy out of reach, so
// GetBody is cleared as a last resort (see redirectStrippingClient).
func noFollowRedirects(c remote.Client) remote.Client {
	switch client := c.(type) {
	case *auth.Client:
		copyClient := *client
		copyClient.Client = redirectSuppressed(client.Client)
		return &copyClient
	case *http.Client:
		return redirectSuppressed(client)
	default:
		return redirectStrippingClient{c}
	}
}

// redirectSuppressed returns a copy of base whose CheckRedirect stops at the
// first response. A nil base is treated as http.DefaultClient (the client an
// auth.Client resolves to when its inner client is unset), so a customized
// default transport, proxy, TLS, timeout or cookie jar is preserved rather than
// replaced by a bare client.
func redirectSuppressed(base *http.Client) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	copyClient := *base
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &copyClient
}

// redirectStrippingClient wraps a remote.Client whose redirect policy is out of
// reach and clears Request.GetBody on body-carrying requests so the standard
// library declines to replay a 307/308 body across a redirect (see net/http
// redirectBehavior). This is a last resort for a custom remote.Client: unlike
// the *auth.Client and *http.Client paths it also disables body rewind, so such
// a client cannot re-send a chunk after a 401 challenge.
type redirectStrippingClient struct{ remote.Client }

func (c redirectStrippingClient) Do(req *http.Request) (*http.Response, error) {
	if req.Body != nil && req.Body != http.NoBody {
		req.GetBody = nil
	}
	return c.Client.Do(req)
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

// cancelTimeout bounds the best-effort DELETE that releases a failed upload
// session.
const cancelTimeout = 5 * time.Second

// cancelUpload issues a best-effort DELETE to release an in-progress upload
// session. Per the OCI spec, clients SHOULD ignore any failures. It runs on a
// context detached from ctx's cancellation, so a session is also released when
// the upload failed because ctx was cancelled, and is bounded by cancelTimeout.
func (r *RemoteStore) cancelUpload(ctx context.Context, location *url.URL) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cancelTimeout)
	defer cancel()
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
