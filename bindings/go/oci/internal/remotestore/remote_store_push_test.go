package remotestore

import (
	"bytes"
	"context"
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
	t                 *testing.T
	mu                sync.Mutex
	requests          []recordedRequest
	sessionID         int
	uploaded          []byte
	monolithicBlobs   map[string][]byte
	rejectUploadPOST  bool
	patchLimit        int // PATCH bodies above this many bytes are rejected with patchRejectStatus; 0 = no limit
	patchRejectStatus int // status for an oversized PATCH; 0 = 416, as ghcr.io answers
	chunkMinLength    string
	warning           string
	patchAcceptStatus int    // status for an accepted PATCH; 0 = 202. ECR answers 201 with a stale Range header.
	dropPatchConn     bool   // close the connection on PATCH without sending a response
	rejectPUT         bool   // answer the closing PUT with 400
	onPatch           func() // called while handling each PATCH, before responding
}

func newChunkedRegistry(t *testing.T) *chunkedRegistry {
	t.Helper()
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
				w.Header().Set("Oci-Chunk-Min-Length", reg.chunkMinLength)
			}
			if reg.warning != "" {
				w.Header().Set("Warning", reg.warning)
			}
			w.Header().Set("Location", fmt.Sprintf("/v2/test-repo/blobs/uploads/%d", id))
			w.WriteHeader(http.StatusAccepted)

		case r.Method == http.MethodPatch:
			if reg.onPatch != nil {
				reg.onPatch()
			}
			if reg.dropPatchConn {
				if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
					_ = conn.Close()
				}
				return
			}
			if reg.patchLimit > 0 && len(body) > reg.patchLimit {
				status := reg.patchRejectStatus
				if status == 0 {
					status = http.StatusRequestedRangeNotSatisfiable
				}
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"errors":[{"code":"RANGE_INVALID","message":"the request body is too large"}]}`)
				return
			}
			reg.mu.Lock()
			reg.uploaded = append(reg.uploaded, body...)
			reg.mu.Unlock()
			w.Header().Set("Location", r.URL.Path)
			if reg.patchAcceptStatus != 0 {
				w.Header().Set("Range", "0-0")
				w.WriteHeader(reg.patchAcceptStatus)
				return
			}
			w.WriteHeader(http.StatusAccepted)

		case r.Method == http.MethodPut:
			if reg.rejectPUT {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
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

func newTestStore(t *testing.T, srv *httptest.Server, chunkSize int64) *RemoteStore {
	t.Helper()
	repo, err := remote.NewRepository(srv.Listener.Addr().String() + "/test-repo")
	require.NoError(t, err)
	repo.PlainHTTP = true
	repo.Client = &http.Client{}
	return &RemoteStore{Repository: repo, ChunkSize: chunkSize}
}

// TestRemoteStore_Push_LargeBlobStaysMonolithic verifies that Push of a blob
// larger than ChunkSize with chunking enabled performs only POST then PUT (no
// PATCH) and the registry stores the full blob under its digest.
func TestRemoteStore_Push_LargeBlobStaysMonolithic(t *testing.T) {
	r := require.New(t)

	reg := newChunkedRegistry(t)
	srv := httptest.NewServer(reg.monolithicHandler())
	t.Cleanup(srv.Close)

	data := bytes.Repeat([]byte("x"), 40) // 40 bytes, well above ChunkSize 4
	dig := digest.FromBytes(data)
	desc := ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: dig, Size: int64(len(data))}

	store := newTestStore(t, srv, 4)
	r.NoError(store.Push(t.Context(), desc, bytes.NewReader(data)))

	// Push is always monolithic: POST then PUT, no PATCH.
	r.Equal([]string{http.MethodPost, http.MethodPut}, reg.methods())
	r.Equal(data, reg.monolithicBlobs[dig.String()])
}

// TestRemoteStore_PushStreaming_CompleteDescriptorIsMonolithic verifies that
// PushStreaming with a complete descriptor (digest+size) performs only POST then
// PUT, returns the descriptor, and issues no PATCH.
func TestRemoteStore_PushStreaming_CompleteDescriptorIsMonolithic(t *testing.T) {
	r := require.New(t)

	reg := newChunkedRegistry(t)
	srv := httptest.NewServer(reg.monolithicHandler())
	t.Cleanup(srv.Close)

	data := bytes.Repeat([]byte("y"), 40)
	dig := digest.FromBytes(data)
	desc := ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: dig, Size: int64(len(data))}

	store := newTestStore(t, srv, 4)
	got, err := store.PushStreaming(t.Context(), desc, bytes.NewReader(data))
	r.NoError(err)
	r.Equal(desc, got)

	// Monolithic: POST then PUT, no PATCH.
	r.Equal([]string{http.MethodPost, http.MethodPut}, reg.methods())
	r.Equal(data, reg.monolithicBlobs[dig.String()])
}

// TestRemoteStore_PushStreaming_ErrorsWhenPATCHRejectedAfterAcceptedChunk
// verifies that a registry rejecting the second PATCH after accepting the first
// causes an error (and a DELETE to cancel the session) rather than a silent
// success, because the reader cannot be rewound.
func TestRemoteStore_PushStreaming_ErrorsWhenPATCHRejectedAfterAcceptedChunk(t *testing.T) {
	r := require.New(t)

	reg := newChunkedRegistry(t)
	var patches int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		reg.record(recordedRequest{method: req.Method, path: req.URL.Path, body: body})
		switch {
		case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/blobs/uploads/"):
			w.Header().Set("Location", "/v2/test-repo/blobs/uploads/1")
			w.WriteHeader(http.StatusAccepted)
		case req.Method == http.MethodPatch:
			reg.mu.Lock()
			patches++
			n := patches
			reg.mu.Unlock()
			if n > 1 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Location", req.URL.Path)
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	data := bytes.Repeat([]byte("x"), 10)

	store := newTestStore(t, srv, 4)
	_, err := store.PushStreaming(t.Context(),
		ociImageSpecV1.Descriptor{MediaType: "application/octet-stream"},
		bytes.NewReader(data))
	r.Error(err)
	r.Contains(err.Error(), "unexpected PATCH status 400 after 4 bytes")
	r.Equal([]string{http.MethodPost, http.MethodPatch, http.MethodPatch, http.MethodDelete}, reg.methods())
}

// patchSizes returns the body size of every PATCH the registry received,
// including rejected ones.
func (reg *chunkedRegistry) patchSizes() []int {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	var sizes []int
	for _, req := range reg.requests {
		if req.method == http.MethodPatch {
			sizes = append(sizes, len(req.body))
		}
	}
	return sizes
}

// TestRemoteStore_PushStreaming_RetriesRejectedChunkSmaller covers registries
// and proxies that cap the PATCH body below the chunk size (ghcr.io answers
// 416 above 4 MiB, proxies 413). The rejected chunk is still in the chunk
// buffer, so it is resent in halves on the same session, and the smaller size
// is kept for the rest of the upload: one session, no buffering of the blob.
func TestRemoteStore_PushStreaming_RetriesRejectedChunkSmaller(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789abcdef"), (1<<20+100<<10)/16)
	dig := digest.FromBytes(data)
	mediaType := "application/octet-stream"

	tests := []struct {
		name    string
		status  int
		partial ociImageSpecV1.Descriptor
	}{
		{name: "416 with unknown digest and size", status: http.StatusRequestedRangeNotSatisfiable, partial: ociImageSpecV1.Descriptor{MediaType: mediaType}},
		{name: "413 with unknown digest and size", status: http.StatusRequestEntityTooLarge, partial: ociImageSpecV1.Descriptor{MediaType: mediaType}},
		{name: "416 with known digest, unknown size", status: http.StatusRequestedRangeNotSatisfiable, partial: ociImageSpecV1.Descriptor{MediaType: mediaType, Digest: dig}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			reg := newChunkedRegistry(t)
			reg.patchLimit = 300 << 10
			reg.patchRejectStatus = tc.status
			srv := httptest.NewServer(reg.handler())
			t.Cleanup(srv.Close)

			got, err := newTestStore(t, srv, 1<<20).PushStreaming(t.Context(), tc.partial, bytes.NewReader(data))
			r.NoError(err)
			r.Equal(ociImageSpecV1.Descriptor{MediaType: mediaType, Digest: dig, Size: int64(len(data))}, got)

			// 1 MiB and 512 KiB are rejected, then 256 KiB fits and is kept.
			r.Equal([]int{1 << 20, 512 << 10, 256 << 10, 256 << 10, 256 << 10, 256 << 10, 100 << 10}, reg.patchSizes())
			methods := reg.methods()
			r.Equal(http.MethodPost, methods[0])
			r.Equal(http.MethodPut, methods[len(methods)-1])
			r.NotContains(methods[1:], http.MethodPost, "the retry must stay on the same session")
			r.NotContains(methods, http.MethodDelete)
			r.Equal(data, reg.uploaded)
		})
	}
}

// TestRemoteStore_PushStreaming_RejectedChunkBelowFloorFails verifies that
// halving stops at MinRetryChunkSize or the registry's OCI-Chunk-Min-Length:
// the upload then fails and the session is cancelled without committing.
func TestRemoteStore_PushStreaming_RejectedChunkBelowFloorFails(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 2<<20)
	tests := []struct {
		name           string
		patchLimit     int
		chunkMinLength string
		wantPatchSizes []int
	}{
		{name: "limit below MinRetryChunkSize", patchLimit: 200 << 10, wantPatchSizes: []int{1 << 20, 512 << 10, 256 << 10}},
		{name: "limit below advertised minimum", patchLimit: 600 << 10, chunkMinLength: "786432", wantPatchSizes: []int{1 << 20}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			reg := newChunkedRegistry(t)
			reg.patchLimit = tc.patchLimit
			reg.chunkMinLength = tc.chunkMinLength
			srv := httptest.NewServer(reg.handler())
			t.Cleanup(srv.Close)

			_, err := newTestStore(t, srv, 1<<20).PushStreaming(t.Context(),
				ociImageSpecV1.Descriptor{MediaType: "application/octet-stream"}, bytes.NewReader(data))
			r.ErrorContains(err, "unexpected PATCH status 416 after 0 bytes")
			r.Equal(tc.wantPatchSizes, reg.patchSizes())
			r.Equal(http.MethodDelete, reg.methods()[len(reg.methods())-1])
			r.NotContains(reg.methods(), http.MethodPut)
		})
	}
}

// TestRemoteStore_PushStreaming_HonorsChunkMinLength verifies that a
// registry-advertised OCI-Chunk-Min-Length raises the effective chunk size.
func TestRemoteStore_PushStreaming_HonorsChunkMinLength(t *testing.T) {
	r := require.New(t)

	reg := newChunkedRegistry(t)
	reg.chunkMinLength = "4"
	srv := httptest.NewServer(reg.handler())
	t.Cleanup(srv.Close)

	data := bytes.Repeat([]byte("a"), 9) // 9 bytes

	store := newTestStore(t, srv, 2 /*chunk below min*/)
	got, err := store.PushStreaming(t.Context(),
		ociImageSpecV1.Descriptor{MediaType: "application/octet-stream"},
		bytes.NewReader(data))
	r.NoError(err)
	r.Equal(digest.FromBytes(data), got.Digest)

	// Chunk size raised to min length 4 => chunks 4,4,1.
	r.Equal([]string{
		http.MethodPost,
		http.MethodPatch, http.MethodPatch, http.MethodPatch,
		http.MethodPut,
	}, reg.methods())
	reqs := reg.requests
	r.Equal("0-3", reqs[1].contentRange)
	r.Equal("4-7", reqs[2].contentRange)
	r.Equal("8-8", reqs[3].contentRange)
	r.Equal(data, reg.uploaded)
}

func TestRemoteStore_PushStreaming_ComputesDigest(t *testing.T) {
	r := require.New(t)

	reg := newChunkedRegistry(t)
	srv := httptest.NewServer(reg.handler())
	t.Cleanup(srv.Close)

	data := bytes.Repeat([]byte("stream"), 5) // 30 bytes, digest unknown to caller
	wantDigest := digest.FromBytes(data)

	store := newTestStore(t, srv, 8)
	got, err := store.PushStreaming(t.Context(),
		ociImageSpecV1.Descriptor{MediaType: "application/octet-stream"},
		bytes.NewReader(data))
	r.NoError(err)

	// Descriptor is completed from the streamed bytes.
	r.Equal(wantDigest, got.Digest)
	r.Equal(int64(len(data)), got.Size)
	r.Equal("application/octet-stream", got.MediaType)

	// The session was closed with the computed digest and the bytes match.
	r.Equal(wantDigest.String(), reg.requests[len(reg.requests)-1].digestQuery)
	r.Equal(data, reg.uploaded)
}

func TestRemoteStore_PushStreaming_KnownDigestUnknownSize(t *testing.T) {
	r := require.New(t)

	// A lazily-loaded blob that exposes a digest but not a size streams without
	// buffering: the size is discovered during upload and the session closes
	// with the caller-supplied digest.
	reg := newChunkedRegistry(t)
	srv := httptest.NewServer(reg.handler())
	t.Cleanup(srv.Close)

	data := bytes.Repeat([]byte("known"), 6) // 30 bytes
	dig := digest.FromBytes(data)

	store := newTestStore(t, srv, 8)
	got, err := store.PushStreaming(t.Context(),
		ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: dig, Size: 0 /*unknown*/},
		bytes.NewReader(data))
	r.NoError(err)

	r.Equal(dig, got.Digest)
	r.Equal(int64(len(data)), got.Size)
	r.Equal(dig.String(), reg.requests[len(reg.requests)-1].digestQuery)
	r.Equal(data, reg.uploaded)
}

func TestRemoteStore_PushStreaming_DigestMismatchErrors(t *testing.T) {
	r := require.New(t)

	// A claimed digest that does not match the streamed bytes must fail rather
	// than close the session under a false identifier.
	reg := newChunkedRegistry(t)
	srv := httptest.NewServer(reg.handler())
	t.Cleanup(srv.Close)

	data := bytes.Repeat([]byte("real"), 8)
	wrong := digest.FromString("not the content")

	store := newTestStore(t, srv, 8)
	_, err := store.PushStreaming(t.Context(),
		ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: wrong},
		bytes.NewReader(data))
	r.Error(err)
	r.Contains(err.Error(), "does not match declared digest")
	// The session must not be closed with the wrong digest.
	r.NotContains(reg.methods(), http.MethodPut)
}

// TestRemoteStore_PushStreaming_ZeroChunkSizeUsesDefault verifies that an
// unset ChunkSize still streams, with DefaultChunkSize chunks, so a resolver
// without WithChunkedPush never falls back to buffering the blob.
func TestRemoteStore_PushStreaming_ZeroChunkSizeUsesDefault(t *testing.T) {
	r := require.New(t)

	reg := newChunkedRegistry(t)
	srv := httptest.NewServer(reg.handler())
	t.Cleanup(srv.Close)

	data := bytes.Repeat([]byte("d"), int(DefaultChunkSize)+1<<20)
	got, err := newTestStore(t, srv, 0).PushStreaming(t.Context(),
		ociImageSpecV1.Descriptor{MediaType: "application/octet-stream"}, bytes.NewReader(data))
	r.NoError(err)
	r.Equal(digest.FromBytes(data), got.Digest)
	r.Equal([]int{int(DefaultChunkSize), 1 << 20}, reg.patchSizes())
	r.Equal(data, reg.uploaded)
}

// TestRemoteStore_PushStreaming_AcceptsAny2xxForPATCH covers registries that
// acknowledge a chunk with a 2xx other than the spec's 202: ECR answers 201
// with a Range header that does not advance. The offset is tracked from what
// was sent, so the upload completes with the right content.
func TestRemoteStore_PushStreaming_AcceptsAny2xxForPATCH(t *testing.T) {
	for _, status := range []int{http.StatusCreated, http.StatusNoContent} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			r := require.New(t)
			reg := newChunkedRegistry(t)
			reg.patchAcceptStatus = status
			srv := httptest.NewServer(reg.handler())
			t.Cleanup(srv.Close)

			data := []byte("hello chunked world")
			got, err := newTestStore(t, srv, 4).PushStreaming(t.Context(),
				ociImageSpecV1.Descriptor{MediaType: "application/octet-stream"}, bytes.NewReader(data))
			r.NoError(err)
			r.Equal(digest.FromBytes(data), got.Digest)
			r.Equal([]int{4, 4, 4, 4, 3}, reg.patchSizes())
			r.Equal(data, reg.uploaded)
		})
	}
}

// TestRemoteStore_PushStreaming_POSTRejectionFails verifies that a registry
// refusing to open an upload session fails the push: there is no fallback that
// would buffer the blob, and no chunk is sent.
func TestRemoteStore_PushStreaming_POSTRejectionFails(t *testing.T) {
	r := require.New(t)

	reg := newChunkedRegistry(t)
	reg.rejectUploadPOST = true
	srv := httptest.NewServer(reg.handler())
	t.Cleanup(srv.Close)

	store := newTestStore(t, srv, 4)
	_, err := store.PushStreaming(t.Context(),
		ociImageSpecV1.Descriptor{MediaType: "application/octet-stream"},
		strings.NewReader("some data here"))
	r.ErrorContains(err, "failed to open upload session")
	r.Equal([]string{http.MethodPost}, reg.methods())
}

// TestRemoteStore_PushStreaming_InvokesHandleWarning verifies that chunked push
// honors the repository's HandleWarning callback on response Warning headers.
func TestRemoteStore_PushStreaming_InvokesHandleWarning(t *testing.T) {
	r := require.New(t)

	reg := newChunkedRegistry(t)
	reg.warning = `299 - "this repository is deprecated"`
	srv := httptest.NewServer(reg.handler())
	t.Cleanup(srv.Close)

	var warnings []string
	store := newTestStore(t, srv, 2)
	store.HandleWarning = func(w remote.Warning) { warnings = append(warnings, w.Text) }

	data := []byte("hello")
	got, err := store.PushStreaming(t.Context(),
		ociImageSpecV1.Descriptor{MediaType: "application/octet-stream"},
		bytes.NewReader(data))
	r.NoError(err)
	r.Equal(digest.FromBytes(data), got.Digest)

	r.Equal([]string{"this repository is deprecated"}, warnings)
}

// TestRemoteStore_PushStreaming_ChunkedNonSHA256Digest verifies that a chunked
// push of a blob declared with a non-sha256 digest computes the running digest
// with the declared algorithm and closes the session under that same digest.
func TestRemoteStore_PushStreaming_ChunkedNonSHA256Digest(t *testing.T) {
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

	store := newTestStore(t, srv, 4)

	data := bytes.Repeat([]byte("x"), 10)
	sum := sha512.Sum512(data)
	dig := digest.Digest(fmt.Sprintf("sha512:%x", sum))

	// Known digest, unknown size: still chunks, uses the declared algorithm.
	got, err := store.PushStreaming(t.Context(),
		ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: dig, Size: 0},
		bytes.NewReader(data))
	r.NoError(err)
	r.Equal(dig, got.Digest)
	r.Equal("sha512", algoParam)
	r.Equal(dig.String(), closedDigest, "session must be closed with the sha512 digest")
}

// TestRemoteStore_PushStreaming_RejectsOversizedChunkMinLength verifies that an
// implausible registry-advertised OCI-Chunk-Min-Length does not make the client
// allocate a matching buffer: the push fails before any chunk is sent.
func TestRemoteStore_PushStreaming_RejectsOversizedChunkMinLength(t *testing.T) {
	r := require.New(t)

	reg := newChunkedRegistry(t)
	reg.chunkMinLength = fmt.Sprintf("%d", MaxChunkSize+1)
	srv := httptest.NewServer(reg.handler())
	t.Cleanup(srv.Close)

	store := newTestStore(t, srv, 4)
	_, err := store.PushStreaming(t.Context(),
		ociImageSpecV1.Descriptor{MediaType: "application/octet-stream"},
		bytes.NewReader(bytes.Repeat([]byte("a"), 40)))
	r.ErrorContains(err, "advertised OCI-Chunk-Min-Length")
	r.NotContains(reg.methods(), http.MethodPatch)
}

// TestRemoteStore_PushStreaming_RejectsOversizedChunkSize verifies that a
// configured ChunkSize above MaxChunkSize is rejected before any byte is
// consumed, so the client never allocates an unbounded PATCH buffer via
// make([]byte, up.chunk).
func TestRemoteStore_PushStreaming_RejectsOversizedChunkSize(t *testing.T) {
	r := require.New(t)

	reg := newChunkedRegistry(t)
	srv := httptest.NewServer(reg.handler())
	t.Cleanup(srv.Close)

	store := newTestStore(t, srv, MaxChunkSize+1 /*oversized configured chunk*/)
	_, err := store.PushStreaming(t.Context(),
		ociImageSpecV1.Descriptor{MediaType: "application/octet-stream"},
		bytes.NewReader(bytes.Repeat([]byte("a"), 40)))
	r.ErrorContains(err, "exceeds the maximum supported chunk size")

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
	// non-EOF error, exercising the mid-read failure path.
	return n, e.err
}

// TestRemoteStore_PushStreaming_MidStreamReadErrorCancelsSession verifies that
// a read error accompanying already-yielded bytes fails the push with the
// reader's error and cancels the session without committing anything.
func TestRemoteStore_PushStreaming_MidStreamReadErrorCancelsSession(t *testing.T) {
	r := require.New(t)

	reg := newChunkedRegistry(t)
	srv := httptest.NewServer(reg.handler())
	t.Cleanup(srv.Close)

	store := newTestStore(t, srv, 8)
	// Chunk size (8) exceeds the 4 bytes the reader yields, so io.ReadFull
	// returns n=4 together with the reader's non-EOF error in a single call.
	rdr := &errAfterReader{data: bytes.Repeat([]byte("a"), 4), err: fmt.Errorf("boom")}
	_, err := store.PushStreaming(t.Context(),
		ociImageSpecV1.Descriptor{MediaType: "application/octet-stream"}, rdr)
	r.ErrorContains(err, "boom")
	r.Equal([]string{http.MethodPost, http.MethodDelete}, reg.methods())
}

// TestRemoteStore_PushStreaming_FailureCancelsSession verifies that once the
// upload session is open, failures without a usable registry answer release it
// too: a dropped connection, a rejected closing PUT, and a cancelled context,
// whose DELETE must still go out.
func TestRemoteStore_PushStreaming_FailureCancelsSession(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(reg *chunkedRegistry, cancel context.CancelFunc)
		check       func(r *require.Assertions, err error)
		wantMethods []string
	}{
		{
			name:        "connection dropped on PATCH",
			setup:       func(reg *chunkedRegistry, _ context.CancelFunc) { reg.dropPatchConn = true },
			check:       func(r *require.Assertions, err error) { r.ErrorContains(err, "PATCH failed after 0 bytes") },
			wantMethods: []string{http.MethodPost, http.MethodPatch, http.MethodDelete},
		},
		{
			name:        "closing PUT rejected",
			setup:       func(reg *chunkedRegistry, _ context.CancelFunc) { reg.rejectPUT = true },
			check:       func(r *require.Assertions, err error) { r.ErrorContains(err, "unexpected PUT status 400") },
			wantMethods: []string{http.MethodPost, http.MethodPatch, http.MethodPatch, http.MethodPatch, http.MethodPut, http.MethodDelete},
		},
		{
			name:        "context cancelled mid-upload",
			setup:       func(reg *chunkedRegistry, cancel context.CancelFunc) { reg.onPatch = cancel },
			check:       func(r *require.Assertions, err error) { r.ErrorIs(err, context.Canceled) },
			wantMethods: []string{http.MethodPost, http.MethodPatch, http.MethodDelete},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			reg := newChunkedRegistry(t)
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			tc.setup(reg, cancel)
			srv := httptest.NewServer(reg.handler())
			t.Cleanup(srv.Close)

			_, err := newTestStore(t, srv, 4).PushStreaming(ctx,
				ociImageSpecV1.Descriptor{MediaType: "application/octet-stream"},
				bytes.NewReader([]byte("0123456789")))
			tc.check(r, err)
			r.Equal(tc.wantMethods, reg.methods())
		})
	}
}

// TestRemoteStore_PushStreaming_DoesNotFollowPATCHRedirectToOtherHost verifies
// that a registry-controlled 307 redirect on a body-carrying PATCH is not
// transparently followed by the HTTP client — which would replay the blob bytes
// to an unvalidated host (SSRF, CWE-918). The push must fail without the
// attacker host ever receiving the chunk body.
func TestRemoteStore_PushStreaming_DoesNotFollowPATCHRedirectToOtherHost(t *testing.T) {
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
			store := &RemoteStore{Repository: repo, ChunkSize: 8}

			data := bytes.Repeat([]byte("secret"), 8) // 48 bytes
			_, err = store.PushStreaming(t.Context(),
				ociImageSpecV1.Descriptor{MediaType: "application/octet-stream"},
				bytes.NewReader(data))
			r.Error(err, "a redirected PATCH must not silently succeed")
			r.Equal(0, attackerHits, "the redirect target must never be contacted")
			r.Nil(attackerBody, "no blob bytes may reach the redirect target")
		})
	}
}

// TestRemoteStore_PushStreaming_ChunkedThroughAuthClientRewindsBody verifies
// that chunked push works when the underlying client is an oras *auth.Client
// that has to re-send a body-carrying PATCH after a 401 challenge.
func TestRemoteStore_PushStreaming_ChunkedThroughAuthClientRewindsBody(t *testing.T) {
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
	store := &RemoteStore{Repository: repo, ChunkSize: 2}

	// Known digest, unknown size: still chunks.
	got, err := store.PushStreaming(t.Context(),
		ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: dig, Size: 0},
		bytes.NewReader(data))
	r.NoError(err)
	r.Equal(dig, got.Digest)
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
