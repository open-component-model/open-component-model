package remotestore

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry/remote"
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
