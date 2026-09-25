package remotestore

import (
	"bytes"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
)

// recordedRequest captures the salient parts of one registry request.
type recordedRequest struct {
	method       string
	path         string
	contentRange string
	digestQuery  string
	body         []byte
}

// chunkedRegistry is a minimal in-memory registry that implements the OCI
// chunked (and monolithic) blob upload protocol against a single repository. It
// records the request sequence so tests can assert exact protocol behavior.
type chunkedRegistry struct {
	t                *testing.T
	mu               sync.Mutex
	requests         []recordedRequest
	uploaded         []byte // assembled blob bytes across PATCH/PUT
	sessionID        int
	chunkMinLength   string // value advertised via OCI-Chunk-Min-Length; empty = none
	rejectUploadPOST bool   // simulate a registry that does not support the uploads endpoint
	monolithicBlobs  map[string][]byte
	warning          string // value emitted via the Warning header on the POST response
}

func newChunkedRegistry(t *testing.T) *chunkedRegistry {
	return &chunkedRegistry{t: t, monolithicBlobs: map[string][]byte{}}
}

func (reg *chunkedRegistry) record(r recordedRequest) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.requests = append(reg.requests, r)
}

func (reg *chunkedRegistry) methods() []string {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	out := make([]string, len(reg.requests))
	for i, req := range reg.requests {
		out[i] = req.method
	}
	return out
}

func (reg *chunkedRegistry) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		reg.record(recordedRequest{
			method:       r.Method,
			path:         r.URL.Path,
			contentRange: r.Header.Get("Content-Range"),
			digestQuery:  r.URL.Query().Get("digest"),
			body:         body,
		})

		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/blobs/uploads/"):
			// Single-POST monolithic upload with digest query is not offered;
			// clients must use POST-then-PATCH/PUT or POST-then-PUT.
			if reg.rejectUploadPOST {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			reg.mu.Lock()
			reg.sessionID++
			id := reg.sessionID
			reg.uploaded = nil
			reg.mu.Unlock()
			if reg.chunkMinLength != "" {
				w.Header().Set("OCI-Chunk-Min-Length", reg.chunkMinLength)
			}
			if reg.warning != "" {
				w.Header().Set("Warning", reg.warning)
			}
			w.Header().Set("Location", fmt.Sprintf("/v2/test-repo/blobs/uploads/%d", id))
			w.WriteHeader(http.StatusAccepted)

		case r.Method == http.MethodPatch:
			reg.mu.Lock()
			reg.uploaded = append(reg.uploaded, body...)
			reg.mu.Unlock()
			w.Header().Set("Location", r.URL.Path)
			w.WriteHeader(http.StatusAccepted)

		case r.Method == http.MethodPut:
			// Close chunked session; body may carry a final chunk (unused here).
			reg.mu.Lock()
			reg.uploaded = append(reg.uploaded, body...)
			reg.mu.Unlock()
			w.Header().Set("Location", r.URL.Path)
			w.WriteHeader(http.StatusCreated)

		default:
			w.WriteHeader(http.StatusOK)
		}
	}
}

// monolithicHandler serves only the oras monolithic POST-then-PUT flow: the
// uploads POST returns a session, the PUT stores the blob. It records requests
// so a test can assert no PATCH was ever issued.
func (reg *chunkedRegistry) monolithicHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		reg.record(recordedRequest{method: r.Method, path: r.URL.Path, digestQuery: r.URL.Query().Get("digest"), body: body})
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/blobs/uploads/"):
			reg.mu.Lock()
			reg.sessionID++
			id := reg.sessionID
			reg.mu.Unlock()
			w.Header().Set("Location", fmt.Sprintf("/v2/test-repo/blobs/uploads/%d", id))
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPut:
			reg.mu.Lock()
			reg.monolithicBlobs[r.URL.Query().Get("digest")] = body
			reg.mu.Unlock()
			w.Header().Set("Location", r.URL.Path)
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}
}

func newTestStore(t *testing.T, srv *httptest.Server, chunkSize, threshold int64) *RemoteStore {
	t.Helper()
	repo, err := remote.NewRepository(srv.Listener.Addr().String() + "/test-repo")
	require.NoError(t, err)
	repo.PlainHTTP = true
	repo.Client = &http.Client{}
	return &RemoteStore{Repository: repo, ChunkSize: chunkSize, ChunkThreshold: threshold}
}

func TestRemoteStore_Push_Chunked(t *testing.T) {
	reg := newChunkedRegistry(t)
	srv := httptest.NewServer(reg.handler())
	t.Cleanup(srv.Close)

	data := []byte("hello") // 5 bytes
	dig := digest.FromBytes(data)
	desc := ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: dig, Size: int64(len(data))}

	store := newTestStore(t, srv, 2 /*chunk*/, 1 /*threshold*/)
	require.NoError(t, store.Push(t.Context(), desc, bytes.NewReader(data)))

	// Exact protocol: POST, three PATCH (2+2+1), PUT.
	require.Equal(t, []string{
		http.MethodPost,
		http.MethodPatch, http.MethodPatch, http.MethodPatch,
		http.MethodPut,
	}, reg.methods())

	// Content-Range and digest assertions.
	reqs := reg.requests
	require.Equal(t, "0-1", reqs[1].contentRange)
	require.Equal(t, "2-3", reqs[2].contentRange)
	require.Equal(t, "4-4", reqs[3].contentRange)
	require.Equal(t, dig.String(), reqs[4].digestQuery)

	// The registry received exactly the blob bytes.
	require.Equal(t, data, reg.uploaded)
}

func TestRemoteStore_Push_FallbackToMonolithicOnPOSTRejection(t *testing.T) {
	// A registry that 404s the first uploads POST (chunked attempt) then serves
	// the monolithic POST-then-PUT flow. The chunked path fails before any byte
	// is consumed, so Push falls back and still stores the blob. Both chunked and
	// monolithic upload begin with the same POST to /blobs/uploads/; rejecting
	// only the first POST distinguishes the attempts.
	reg := newChunkedRegistry(t)
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		reg.record(recordedRequest{method: r.Method, path: r.URL.Path, digestQuery: r.URL.Query().Get("digest"), body: body})
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/blobs/uploads/"):
			reg.mu.Lock()
			posts++
			n := posts
			reg.mu.Unlock()
			if n == 1 {
				w.WriteHeader(http.StatusNotFound) // reject chunked attempt
				return
			}
			w.Header().Set("Location", "/v2/test-repo/blobs/uploads/1")
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPut:
			reg.mu.Lock()
			reg.monolithicBlobs[r.URL.Query().Get("digest")] = body
			reg.mu.Unlock()
			w.Header().Set("Location", r.URL.Path)
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	data := bytes.Repeat([]byte("x"), 10)
	dig := digest.FromBytes(data)
	desc := ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: dig, Size: int64(len(data))}

	store := newTestStore(t, srv, 4, 1)
	require.NoError(t, store.Push(t.Context(), desc, bytes.NewReader(data)))

	for _, m := range reg.methods() {
		require.NotEqual(t, http.MethodPatch, m, "fallback must not use chunked PATCH")
	}
	require.Equal(t, data, reg.monolithicBlobs[dig.String()])
}

func TestRemoteStore_Push_ErrorsWhenPATCHRejectedAfterConsuming(t *testing.T) {
	// Registry accepts the session POST (202) but rejects the PATCH. Bytes have
	// been consumed by then, so there is no safe monolithic fallback: Push must
	// return an error rather than silently succeed.
	reg := newChunkedRegistry(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		reg.record(recordedRequest{method: r.Method, path: r.URL.Path, body: body})
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/blobs/uploads/"):
			w.Header().Set("Location", "/v2/test-repo/blobs/uploads/1")
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPatch:
			w.WriteHeader(http.StatusBadRequest)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	data := bytes.Repeat([]byte("x"), 10)
	dig := digest.FromBytes(data)
	desc := ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: dig, Size: int64(len(data))}

	store := newTestStore(t, srv, 4, 1)
	err := store.Push(t.Context(), desc, bytes.NewReader(data))
	require.Error(t, err)
	require.Contains(t, err.Error(), "PATCH")
}

func TestRemoteStore_Push_ChunkDisabledIsMonolithic(t *testing.T) {
	reg := newChunkedRegistry(t)
	srv := httptest.NewServer(reg.monolithicHandler())
	t.Cleanup(srv.Close)

	data := bytes.Repeat([]byte("y"), 100)
	dig := digest.FromBytes(data)
	desc := ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: dig, Size: int64(len(data))}

	store := newTestStore(t, srv, 0 /*chunk disabled*/, 0)
	require.NoError(t, store.Push(t.Context(), desc, bytes.NewReader(data)))

	for _, m := range reg.methods() {
		require.NotEqual(t, http.MethodPatch, m, "disabled chunking must not use PATCH")
	}
	require.Equal(t, data, reg.monolithicBlobs[dig.String()])
}

func TestRemoteStore_Push_ThresholdKeepsSmallBlobsMonolithic(t *testing.T) {
	reg := newChunkedRegistry(t)
	srv := httptest.NewServer(reg.monolithicHandler())
	t.Cleanup(srv.Close)

	data := bytes.Repeat([]byte("z"), 10) // below threshold
	dig := digest.FromBytes(data)
	desc := ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: dig, Size: int64(len(data))}

	store := newTestStore(t, srv, 2 /*chunk*/, 1<<20 /*threshold 1MiB*/)
	require.NoError(t, store.Push(t.Context(), desc, bytes.NewReader(data)))

	for _, m := range reg.methods() {
		require.NotEqual(t, http.MethodPatch, m, "sub-threshold blob must not chunk")
	}
	require.Equal(t, data, reg.monolithicBlobs[dig.String()])
}

func TestRemoteStore_Push_SingleChunkBlobStaysMonolithic(t *testing.T) {
	// A blob smaller than one chunk must never chunk, even when ChunkThreshold
	// is configured below ChunkSize: a lone PATCH plus the closing PUT is
	// strictly worse than a monolithic POST/PUT. The effective threshold is
	// raised to ChunkSize.
	reg := newChunkedRegistry(t)
	srv := httptest.NewServer(reg.monolithicHandler())
	t.Cleanup(srv.Close)

	data := bytes.Repeat([]byte("q"), 500) // < ChunkSize
	dig := digest.FromBytes(data)
	desc := ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: dig, Size: int64(len(data))}

	// ChunkThreshold (1) is deliberately below ChunkSize (1024): the floor at
	// ChunkSize must still keep this 500-byte blob monolithic.
	store := newTestStore(t, srv, 1024 /*chunk*/, 1 /*threshold below chunk*/)
	require.NoError(t, store.Push(t.Context(), desc, bytes.NewReader(data)))

	for _, m := range reg.methods() {
		require.NotEqual(t, http.MethodPatch, m, "single-chunk blob must not chunk")
	}
	require.Equal(t, data, reg.monolithicBlobs[dig.String()])
}

func TestRemoteStore_Push_ManifestExcludedFromChunking(t *testing.T) {
	// A manifest descriptor must never take the chunked blob path; it goes to
	// the manifests endpoint via the embedded repository. Serve a valid
	// manifests PUT and assert no PATCH was ever issued.
	reg := newChunkedRegistry(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		reg.record(recordedRequest{method: r.Method, path: r.URL.Path, body: body})
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/manifests/") {
			w.Header().Set("Docker-Content-Digest", r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:])
			w.WriteHeader(http.StatusCreated)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	data := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json"}`)
	dig := digest.FromBytes(data)
	desc := ociImageSpecV1.Descriptor{MediaType: ociImageSpecV1.MediaTypeImageManifest, Digest: dig, Size: int64(len(data))}

	store := newTestStore(t, srv, 2 /*chunk*/, 1 /*threshold*/)
	require.NoError(t, store.Push(t.Context(), desc, bytes.NewReader(data)))

	require.NotEmpty(t, reg.methods())
	for _, m := range reg.methods() {
		require.NotEqual(t, http.MethodPatch, m, "manifest must not chunk")
	}
}

func TestRemoteStore_Push_HonorsChunkMinLength(t *testing.T) {
	reg := newChunkedRegistry(t)
	reg.chunkMinLength = "4"
	srv := httptest.NewServer(reg.handler())
	t.Cleanup(srv.Close)

	data := bytes.Repeat([]byte("a"), 9) // 9 bytes
	dig := digest.FromBytes(data)
	desc := ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: dig, Size: int64(len(data))}

	store := newTestStore(t, srv, 2 /*chunk below min*/, 1)
	require.NoError(t, store.Push(t.Context(), desc, bytes.NewReader(data)))

	// Chunk size raised to min length 4 => chunks 4,4,1.
	require.Equal(t, []string{
		http.MethodPost,
		http.MethodPatch, http.MethodPatch, http.MethodPatch,
		http.MethodPut,
	}, reg.methods())
	reqs := reg.requests
	require.Equal(t, "0-3", reqs[1].contentRange)
	require.Equal(t, "4-7", reqs[2].contentRange)
	require.Equal(t, "8-8", reqs[3].contentRange)
	require.Equal(t, data, reg.uploaded)
}

func TestRemoteStore_PushStreaming_ComputesDigest(t *testing.T) {
	reg := newChunkedRegistry(t)
	srv := httptest.NewServer(reg.handler())
	t.Cleanup(srv.Close)

	data := bytes.Repeat([]byte("stream"), 5) // 30 bytes, digest unknown to caller
	wantDigest := digest.FromBytes(data)

	store := newTestStore(t, srv, 8, 1)
	got, err := store.PushStreaming(t.Context(),
		ociImageSpecV1.Descriptor{MediaType: "application/octet-stream"},
		bytes.NewReader(data))
	require.NoError(t, err)

	// Descriptor is completed from the streamed bytes.
	require.Equal(t, wantDigest, got.Digest)
	require.Equal(t, int64(len(data)), got.Size)
	require.Equal(t, "application/octet-stream", got.MediaType)

	// The session was closed with the computed digest and the bytes match.
	require.Equal(t, wantDigest.String(), reg.requests[len(reg.requests)-1].digestQuery)
	require.Equal(t, data, reg.uploaded)
}

func TestRemoteStore_PushStreaming_KnownDigestUnknownSize(t *testing.T) {
	// A lazily-loaded blob that exposes a digest but not a size streams without
	// buffering: the size is discovered during upload and the session closes
	// with the caller-supplied digest.
	reg := newChunkedRegistry(t)
	srv := httptest.NewServer(reg.handler())
	t.Cleanup(srv.Close)

	data := bytes.Repeat([]byte("known"), 6) // 30 bytes
	dig := digest.FromBytes(data)

	store := newTestStore(t, srv, 8, 1)
	got, err := store.PushStreaming(t.Context(),
		ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: dig, Size: 0 /*unknown*/},
		bytes.NewReader(data))
	require.NoError(t, err)

	require.Equal(t, dig, got.Digest)
	require.Equal(t, int64(len(data)), got.Size)
	require.Equal(t, dig.String(), reg.requests[len(reg.requests)-1].digestQuery)
	require.Equal(t, data, reg.uploaded)
}

func TestRemoteStore_PushStreaming_DigestMismatchErrors(t *testing.T) {
	// A claimed digest that does not match the streamed bytes must fail rather
	// than close the session under a false identifier.
	reg := newChunkedRegistry(t)
	srv := httptest.NewServer(reg.handler())
	t.Cleanup(srv.Close)

	data := bytes.Repeat([]byte("real"), 8)
	wrong := digest.FromString("not the content")

	store := newTestStore(t, srv, 8, 1)
	_, err := store.PushStreaming(t.Context(),
		ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: wrong},
		bytes.NewReader(data))
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not match declared digest")
	// The session must not be closed with the wrong digest.
	require.NotContains(t, reg.methods(), http.MethodPut)
}

func TestRemoteStore_PushStreaming_UnavailableWhenChunkingDisabled(t *testing.T) {
	reg := newChunkedRegistry(t)
	srv := httptest.NewServer(reg.handler())
	t.Cleanup(srv.Close)

	store := newTestStore(t, srv, 0 /*disabled*/, 0)
	_, err := store.PushStreaming(t.Context(),
		ociImageSpecV1.Descriptor{MediaType: "application/octet-stream"},
		strings.NewReader("data"))
	require.ErrorIs(t, err, ErrStreamingUnavailable)
	require.Empty(t, reg.requests, "no request should be made when streaming is unavailable")
}

func TestRemoteStore_PushStreaming_UnavailableOnPOSTRejection(t *testing.T) {
	reg := newChunkedRegistry(t)
	reg.rejectUploadPOST = true
	srv := httptest.NewServer(reg.handler())
	t.Cleanup(srv.Close)

	store := newTestStore(t, srv, 4, 1)
	_, err := store.PushStreaming(t.Context(),
		ociImageSpecV1.Descriptor{MediaType: "application/octet-stream"},
		strings.NewReader("some data here"))
	// Pre-consumption failure with no fallback => streaming unavailable, so the
	// caller can buffer and retry via Push.
	require.ErrorIs(t, err, ErrStreamingUnavailable)
}

func TestRemoteStore_Push_InvokesHandleWarning(t *testing.T) {
	// Chunked push must honor the repository's HandleWarning callback on
	// response Warning headers, matching the embedded oras client's behavior.
	reg := newChunkedRegistry(t)
	reg.warning = `299 - "this repository is deprecated"`
	srv := httptest.NewServer(reg.handler())
	t.Cleanup(srv.Close)

	var warnings []string
	store := newTestStore(t, srv, 2, 1)
	store.HandleWarning = func(w remote.Warning) { warnings = append(warnings, w.Text) }

	data := []byte("hello")
	dig := digest.FromBytes(data)
	desc := ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: dig, Size: int64(len(data))}
	require.NoError(t, store.Push(t.Context(), desc, bytes.NewReader(data)))

	require.Equal(t, []string{"this repository is deprecated"}, warnings)
}

// TestRemoteStore_Push_ChunkedNonSHA256Digest verifies that a chunked push of a
// blob declared with a non-sha256 digest computes the running digest with the
// declared algorithm and closes the session under that same digest, rather than
// hashing with sha256 and failing resolveFinalDigest with a mismatch.
func TestRemoteStore_Push_ChunkedNonSHA256Digest(t *testing.T) {
	r := require.New(t)

	var algoParam, closedDigest string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.Method {
		case http.MethodPost:
			algoParam = req.URL.Query().Get("digest-algorithm")
			w.Header().Set("Location", "/v2/test-repo/blobs/uploads/1")
			w.WriteHeader(http.StatusAccepted)
		case http.MethodPatch:
			w.Header().Set("Location", req.URL.Path)
			w.WriteHeader(http.StatusAccepted)
		case http.MethodPut:
			closedDigest = req.URL.Query().Get("digest")
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	store := newTestStore(t, srv, 4, 1)

	data := bytes.Repeat([]byte("x"), 10)
	sum := sha512.Sum512(data)
	dig := digest.Digest(fmt.Sprintf("sha512:%x", sum))
	desc := ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: dig, Size: int64(len(data))}

	r.NoError(store.Push(t.Context(), desc, bytes.NewReader(data)))
	r.Equal("sha512", algoParam)
	r.Equal(dig.String(), closedDigest, "session must be closed with the sha512 digest")
}

// TestRemoteStore_Push_RejectsOversizedChunkMinLength verifies that an
// implausible registry-advertised OCI-Chunk-Min-Length does not make the client
// allocate a matching buffer (which would panic or exhaust memory). The
// rejection happens before any byte is consumed, so Push falls back to the
// monolithic upload rather than failing.
func TestRemoteStore_Push_RejectsOversizedChunkMinLength(t *testing.T) {
	r := require.New(t)

	reg := newChunkedRegistry(t)
	reg.chunkMinLength = fmt.Sprintf("%d", MaxChunkSize+1)
	srv := httptest.NewServer(reg.handler())
	t.Cleanup(srv.Close)

	data := bytes.Repeat([]byte("a"), 40)
	dig := digest.FromBytes(data)
	desc := ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: dig, Size: int64(len(data))}

	store := newTestStore(t, srv, 4, 1)
	r.NoError(store.Push(t.Context(), desc, bytes.NewReader(data)))

	// The chunked path aborted before consuming bytes and fell back to the
	// embedded monolithic push, so no PATCH was ever issued but the blob still
	// uploaded (the fallback's closing PUT carries the whole-blob bytes).
	methods := reg.methods()
	for _, m := range methods {
		r.NotEqual(http.MethodPatch, m, "oversized min-length must abort chunking before any PATCH")
	}
	r.Contains(methods, http.MethodPost, "monolithic fallback still opens a session")
	r.Equal(data, reg.uploaded, "fallback must upload the full blob")
}

// TestRemoteStore_PushStreaming_RejectsOversizedChunkMinLength verifies the same
// bound for streaming, which has no monolithic fallback and therefore reports
// ErrStreamingUnavailable without consuming content.
func TestRemoteStore_PushStreaming_RejectsOversizedChunkMinLength(t *testing.T) {
	r := require.New(t)

	reg := newChunkedRegistry(t)
	reg.chunkMinLength = fmt.Sprintf("%d", MaxChunkSize+1)
	srv := httptest.NewServer(reg.handler())
	t.Cleanup(srv.Close)

	store := newTestStore(t, srv, 4, 1)
	_, err := store.PushStreaming(t.Context(),
		ociImageSpecV1.Descriptor{MediaType: "application/octet-stream"},
		bytes.NewReader(bytes.Repeat([]byte("a"), 40)))
	r.ErrorIs(err, ErrStreamingUnavailable)
}

// TestRemoteStore_PushStreaming_RejectsOversizedChunkSize verifies that a
// user-configured ChunkSize above MaxChunkSize is rejected before any byte is
// consumed, so the client never allocates an unbounded PATCH buffer via
// make([]byte, up.chunk). Streaming has no monolithic fallback, so it reports
// ErrStreamingUnavailable.
func TestRemoteStore_PushStreaming_RejectsOversizedChunkSize(t *testing.T) {
	r := require.New(t)

	reg := newChunkedRegistry(t)
	srv := httptest.NewServer(reg.handler())
	t.Cleanup(srv.Close)

	store := newTestStore(t, srv, MaxChunkSize+1 /*oversized configured chunk*/, 1)
	_, err := store.PushStreaming(t.Context(),
		ociImageSpecV1.Descriptor{MediaType: "application/octet-stream"},
		bytes.NewReader(bytes.Repeat([]byte("a"), 40)))
	r.ErrorIs(err, ErrStreamingUnavailable)

	// The reader is untouched: the bound is checked before the session POST.
	for _, m := range reg.methods() {
		r.NotEqual(http.MethodPatch, m, "oversized configured chunk must abort before any PATCH")
	}
}

// errAfterReader yields data once (n>0) and then fails with a non-EOF error on
// the next read, mimicking a source that errors mid-stream after bytes have
// already been handed out.
type errAfterReader struct {
	data []byte
	err  error
	done bool
}

func (e *errAfterReader) Read(p []byte) (int, error) {
	if e.done {
		return 0, e.err
	}
	e.done = true
	n := copy(p, e.data)
	// Return bytes and the error together so io.ReadFull surfaces n>0 with a
	// non-EOF error, exercising the mid-read consumed-state path.
	return n, e.err
}

// TestRemoteStore_PushStreaming_MidStreamReadErrorIsPostConsumption verifies
// that a read error accompanying already-yielded bytes is treated as
// post-consumption. PushStreaming must surface the read error, not
// ErrStreamingUnavailable — the latter's contract promises no content was
// consumed, which a caller relies on to safely buffer and retry. Reporting it
// after bytes have left the reader would corrupt that retry.
func TestRemoteStore_PushStreaming_MidStreamReadErrorIsPostConsumption(t *testing.T) {
	r := require.New(t)

	reg := newChunkedRegistry(t)
	srv := httptest.NewServer(reg.handler())
	t.Cleanup(srv.Close)

	store := newTestStore(t, srv, 8, 1)
	// Chunk size (8) exceeds the 4 bytes the reader yields, so io.ReadFull
	// returns n=4 together with the reader's non-EOF error in a single call —
	// the exact n>0-with-error path that must count as consumed.
	rdr := &errAfterReader{data: bytes.Repeat([]byte("a"), 4), err: fmt.Errorf("boom")}
	_, err := store.PushStreaming(t.Context(),
		ociImageSpecV1.Descriptor{MediaType: "application/octet-stream"}, rdr)
	r.Error(err)
	r.Contains(err.Error(), "boom")
	r.NotErrorIs(err, ErrStreamingUnavailable,
		"a mid-read error after bytes were consumed must not masquerade as unavailable")
}

// TestRemoteStore_Push_DoesNotFollowPATCHRedirectToOtherHost verifies that a
// registry-controlled 307 redirect on a body-carrying PATCH is not transparently
// followed by the HTTP client — which would replay the blob bytes to an
// unvalidated host (SSRF, CWE-918). The push must fail without the attacker host
// ever receiving the chunk body.
func TestRemoteStore_Push_DoesNotFollowPATCHRedirectToOtherHost(t *testing.T) {
	// Both concrete client types this package special-cases must suppress the
	// redirect: a plain *http.Client and the *auth.Client used against real
	// registries.
	for _, tc := range []struct {
		name   string
		client func() remote.Client
	}{
		{
			name:   "http.Client",
			client: func() remote.Client { return &http.Client{} },
		},
		{
			name:   "auth.Client",
			client: func() remote.Client { return &auth.Client{Client: &http.Client{}, Cache: auth.NewCache()} },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			var attackerBody []byte
			var attackerHits int
			attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				attackerHits++
				attackerBody, _ = io.ReadAll(req.Body)
				w.WriteHeader(http.StatusAccepted)
			}))
			t.Cleanup(attacker.Close)

			registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				switch req.Method {
				case http.MethodPost:
					w.Header().Set("Location", "/v2/test-repo/blobs/uploads/1")
					w.WriteHeader(http.StatusAccepted)
				case http.MethodPatch:
					// Redirect the chunk to the attacker host with a body-preserving 307.
					http.Redirect(w, req, attacker.URL+"/leak", http.StatusTemporaryRedirect)
				default:
					w.WriteHeader(http.StatusOK)
				}
			}))
			t.Cleanup(registry.Close)

			repo, err := remote.NewRepository(registry.Listener.Addr().String() + "/test-repo")
			r.NoError(err)
			repo.PlainHTTP = true
			repo.Client = tc.client()
			store := &RemoteStore{Repository: repo, ChunkSize: 8, ChunkThreshold: 1}

			data := bytes.Repeat([]byte("secret"), 8) // 48 bytes, above threshold
			dig := digest.FromBytes(data)
			desc := ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: dig, Size: int64(len(data))}

			err = store.Push(t.Context(), desc, bytes.NewReader(data))
			r.Error(err, "a redirected PATCH must not silently succeed")
			r.Equal(0, attackerHits, "the redirect target must never be contacted")
			r.Nil(attackerBody, "no blob bytes may reach the redirect target")
		})
	}
}

// TestRemoteStore_Push_ChunkedThroughAuthClientRewindsBody verifies that
// chunked push works when the underlying client is an oras *auth.Client that
// has to re-send a body-carrying PATCH after a 401 challenge. A real registry
// challenges the PATCH independently of the POST that opened the session, so
// auth.Client re-authenticates and rewinds the request body via
// Request.GetBody (auth.Client.Do -> rewindRequestBody). If the chunked path
// cleared GetBody to guard against redirects, that rewind fails with "request
// body is not rewindable" and every PATCH errors after 0 bytes — exactly how
// the real-registry integration test broke. The push must succeed and the
// registry must receive the whole blob.
func TestRemoteStore_Push_ChunkedThroughAuthClientRewindsBody(t *testing.T) {
	r := require.New(t)

	const user, pass = "user", "pass"
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))

	reg := newChunkedRegistry(t)
	var mu sync.Mutex
	patchChallenged := make(map[string]bool)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// The session POST is served anonymously, but each PATCH location is
		// challenged once before it is accepted, forcing auth.Client to rewind
		// and re-send the chunk body with credentials.
		if req.Method == http.MethodPatch {
			mu.Lock()
			seen := patchChallenged[req.URL.Path]
			patchChallenged[req.URL.Path] = true
			mu.Unlock()
			if !seen && req.Header.Get("Authorization") != basic {
				w.Header().Set("WWW-Authenticate", `Basic realm="registry"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}
		reg.handler().ServeHTTP(w, req)
	}))
	t.Cleanup(srv.Close)

	data := []byte("hello") // 5 bytes -> chunks of 2: 2+2+1
	dig := digest.FromBytes(data)
	desc := ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: dig, Size: int64(len(data))}

	repo, err := remote.NewRepository(srv.Listener.Addr().String() + "/test-repo")
	r.NoError(err)
	repo.PlainHTTP = true
	repo.Client = &auth.Client{
		Client: &http.Client{},
		Cache:  auth.NewCache(),
		Credential: auth.StaticCredential(srv.Listener.Addr().String(), auth.Credential{
			Username: user,
			Password: pass,
		}),
	}
	store := &RemoteStore{Repository: repo, ChunkSize: 2, ChunkThreshold: 1}

	r.NoError(store.Push(t.Context(), desc, bytes.NewReader(data)))
	r.Equal(data, reg.uploaded, "the registry must receive the whole blob after auth rewind")
}

// sentinelRoundTripper is a marker transport used to prove client copying.
type sentinelRoundTripper struct{ http.RoundTripper }

func (sentinelRoundTripper) RoundTrip(*http.Request) (*http.Response, error) { return nil, nil }

// TestNoFollowRedirects_PreservesDefaultClientForNilAuthInner verifies that
// wrapping an auth.Client whose inner Client is nil copies http.DefaultClient
// (which auth.Client.client() resolves to) rather than a bare http.Client, so a
// customized default transport, proxy, TLS, timeout, or cookie jar survives the
// redirect-suppressing copy. It also confirms the copy suppresses redirects
// without mutating the original client.
func TestNoFollowRedirects_PreservesDefaultClientForNilAuthInner(t *testing.T) {
	r := require.New(t)

	// Customize the process-wide default client so the copy is observably
	// distinct from a bare &http.Client{}; restore it afterwards.
	prevTransport := http.DefaultClient.Transport
	prevTimeout := http.DefaultClient.Timeout
	sentinel := sentinelRoundTripper{}
	http.DefaultClient.Transport = sentinel
	http.DefaultClient.Timeout = 42 * time.Second
	t.Cleanup(func() {
		http.DefaultClient.Transport = prevTransport
		http.DefaultClient.Timeout = prevTimeout
	})

	original := &auth.Client{} // inner Client nil -> resolves to http.DefaultClient
	wrapped := noFollowRedirects(original)

	ac, ok := wrapped.(*auth.Client)
	r.True(ok, "wrapping an *auth.Client must yield an *auth.Client")
	r.NotNil(ac.Client, "inner client must be populated, not left nil")
	r.Equal(sentinel, ac.Client.Transport, "inner client must inherit http.DefaultClient's transport")
	r.Equal(42*time.Second, ac.Client.Timeout, "inner client must inherit http.DefaultClient's timeout")
	r.NotNil(ac.Client.CheckRedirect, "the copy must install a redirect-suppressing policy")
	r.Nil(original.Client, "the original auth.Client must not be mutated")
}
