package cache

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"

	"ocm.software/open-component-model/bindings/go/oci/internal/remotestore"
)

func TestRepository_Fetch_DelegatesToBlobCache(t *testing.T) {
	c := newTestCache(t, Options{})
	manifest := []byte(`{"schemaVersion":2}`)
	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes(manifest),
		Size:      int64(len(manifest)),
	}
	_, err := c.populate(t.Context(), desc.Digest, desc.Size, bytes.NewReader(manifest))
	require.NoError(t, err)

	// Cache hit serves the file; the embedded *remote.Repository is
	// never reached, so an empty value is fine.
	repo := &Repository{Repository: &remote.Repository{}, BlobCache: c}
	rc, err := repo.Fetch(t.Context(), desc)
	require.NoError(t, err)
	defer rc.Close()
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, manifest, got)
}

func TestRepository_Resolve_DelegatesToReferenceCache(t *testing.T) {
	rc := newTestRefCache(t, Options{})
	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes([]byte("x")),
		Size:      1,
	}
	// Use a fully-qualified Reference so the wrapper computes a stable
	// per-repository namespace; the cache pre-populate must use the
	// same namespace ("registry/repository") to be considered a hit.
	// The reference itself is a digest, the only form the cache may
	// answer without upstream.
	inner := &remote.Repository{
		Reference: registry.Reference{Registry: "ghcr.io", Repository: "owner/repo"},
	}
	ref := desc.Digest.String()
	rc.Add(registry.Reference{Registry: "ghcr.io", Repository: "owner/repo", Reference: ref}, desc)

	repo := &Repository{Repository: inner, ReferenceCache: rc}
	got, err := repo.Resolve(t.Context(), ref)
	require.NoError(t, err)
	assert.Equal(t, desc, got)
}

func TestRepository_Resolve_WithFullReferenceAndNormalization(t *testing.T) {
	rc := newTestRefCache(t, Options{})
	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes([]byte("normalized-test")),
		Size:      15,
	}
	inner := &remote.Repository{
		Reference: registry.Reference{Registry: "ghcr.io", Repository: "owner/repo"},
	}
	repo := &Repository{Repository: inner, ReferenceCache: rc}

	// 1. Add as normalized digest reference to cache
	rc.Add(registry.Reference{
		Registry:   "ghcr.io",
		Repository: "owner/repo",
		Reference:  desc.Digest.String(),
	}, desc)

	// 2. Resolve using fully-qualified reference (should hit, since ParseReference normalizes it to the bare digest)
	gotFull, err := repo.Resolve(t.Context(), "ghcr.io/owner/repo@"+desc.Digest.String())
	require.NoError(t, err)
	assert.Equal(t, desc, gotFull)
}

func TestRepository_Unwrap_ReturnsEmbedded(t *testing.T) {
	inner := &remote.Repository{}
	repo := &Repository{Repository: inner, BlobCache: newTestCache(t, Options{})}
	// Unwrap must reach through the cache AND the chunked store wrapper to the
	// raw oras repository so pack's global-store detection still fires.
	assert.Same(t, inner, repo.Unwrap())
}

// TestRepository_SatisfiesStoreAndStreamingPusher is the regression guard for
// this change: with a cache configured, the decorator must still satisfy the
// streaming-upload interface so blobs of unknown size or digest keep streaming
// instead of being buffered, while preserving the promoted type assertions
// (TagLister / ReferrerLister / Blobs()) used across the codebase.
func TestRepository_SatisfiesStoreAndStreamingPusher(t *testing.T) {
	r := require.New(t)
	repo := &Repository{
		Repository:     &remote.Repository{},
		store:          &remotestore.RemoteStore{ChunkSize: remotestore.DefaultChunkSize},
		BlobCache:      newTestCache(t, Options{}),
		ReferenceCache: newTestRefCache(t, Options{}),
	}

	_, ok := interface{}(repo).(remotestore.StreamingPusher)
	r.True(ok, "cached decorator must expose PushStreaming (chunked streaming survives the cache)")

	_, ok = interface{}(repo).(registry.TagLister)
	r.True(ok, "cached decorator must still promote TagLister")

	_, ok = interface{}(repo).(registry.ReferrerLister)
	r.True(ok, "cached decorator must still promote ReferrerLister")

	_, ok = interface{}(repo).(interface {
		Blobs() registry.BlobStore
	})
	r.True(ok, "cached decorator must still promote Blobs()")

	_, ok = interface{}(repo).(interface{ Unwrap() content.Storage })
	r.True(ok, "cached decorator must expose Unwrap for global-store detection")
}

// TestProxyRepository_NilCachesReturnsChunkedStore: with no cache ProxyRepository
// returns a plain chunked store (wrapping the raw repo) that still exposes
// PushStreaming, so chunking survives even without a cache in the chain.
func TestProxyRepository_NilCachesReturnsChunkedStore(t *testing.T) {
	r := require.New(t)
	inner := &remote.Repository{}
	got := ProxyRepository(inner, nil, nil, remotestore.DefaultChunkSize, remotestore.DefaultChunkThreshold)
	store, ok := got.(*remotestore.RemoteStore)
	r.True(ok, "no-cache path must return the chunked store")
	r.Same(inner, store.Repository)
	_, ok = got.(remotestore.StreamingPusher)
	r.True(ok, "no-cache path must be a StreamingPusher")
}

func TestProxyRepository_BlobCacheOnlyWraps(t *testing.T) {
	r := require.New(t)
	inner := &remote.Repository{}
	c := newTestCache(t, Options{})
	got := ProxyRepository(inner, c, nil, 0, 0)
	wrapped, ok := got.(*Repository)
	r.True(ok)
	r.Same(inner, wrapped.Repository)
	r.Same(c, wrapped.BlobCache)
	r.Nil(wrapped.ReferenceCache)
}

func TestProxyRepository_ReferenceCacheOnlyWraps(t *testing.T) {
	r := require.New(t)
	inner := &remote.Repository{}
	rc := newTestRefCache(t, Options{})
	got := ProxyRepository(inner, nil, rc, 0, 0)
	wrapped, ok := got.(*Repository)
	r.True(ok)
	r.Same(inner, wrapped.Repository)
	r.Nil(wrapped.BlobCache)
	r.Same(rc, wrapped.ReferenceCache)
}

func TestProxyRepository_BothCachesWrap(t *testing.T) {
	r := require.New(t)
	inner := &remote.Repository{}
	c := newTestCache(t, Options{})
	rc := newTestRefCache(t, Options{})
	got := ProxyRepository(inner, c, rc, 0, 0)
	wrapped, ok := got.(*Repository)
	r.True(ok)
	r.Same(inner, wrapped.Repository)
	r.Same(c, wrapped.BlobCache)
	r.Same(rc, wrapped.ReferenceCache)
}

// TestProxyRepository_WrapsWithChunkConfig: with a cache, the decorator carries
// the chunk config and still exposes the chunked streaming push, so chunking
// survives the cache.
func TestProxyRepository_WrapsWithChunkConfig(t *testing.T) {
	r := require.New(t)
	inner := &remote.Repository{}
	c := newTestCache(t, Options{})
	got := ProxyRepository(inner, c, nil, remotestore.DefaultChunkSize, remotestore.DefaultChunkThreshold)
	wrapped, ok := got.(*Repository)
	r.True(ok)
	r.Same(inner, wrapped.Repository)
	r.Equal(remotestore.DefaultChunkSize, wrapped.store.ChunkSize)
	r.Equal(remotestore.DefaultChunkThreshold, wrapped.store.ChunkThreshold)
	_, ok = got.(remotestore.StreamingPusher)
	r.True(ok, "cache-wrapped chunking path must be a StreamingPusher")
}

// TestRepository_PushChunksFromThresholdUnderCache guards that Push goes
// through the chunked store when a cache is configured, which is the CLI
// default: the promoted embedded Push would upload every blob in one request.
func TestRepository_PushChunksFromThresholdUnderCache(t *testing.T) {
	r := require.New(t)
	var mu sync.Mutex
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = io.Copy(io.Discard, req.Body)
		mu.Lock()
		methods = append(methods, req.Method)
		mu.Unlock()
		switch req.Method {
		case http.MethodPost:
			w.Header().Set("Location", "/v2/test-repo/blobs/uploads/1")
			w.WriteHeader(http.StatusAccepted)
		case http.MethodPatch:
			w.Header().Set("Location", req.URL.Path)
			w.WriteHeader(http.StatusAccepted)
		case http.MethodPut:
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	store := ProxyRepository(plainHTTPRepo(t, srv), newTestCache(t, Options{}), nil, 4 /*chunk*/, 8 /*threshold*/)
	data := []byte("0123456789")
	r.NoError(store.Push(t.Context(),
		ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: digest.FromBytes(data), Size: int64(len(data))},
		bytes.NewReader(data)))
	r.Equal([]string{http.MethodPost, http.MethodPatch, http.MethodPatch, http.MethodPatch, http.MethodPut}, methods)
}

// plainHTTPRepo builds a *remote.Repository pointed at srv over plain
// HTTP with the standard "<addr>/test-repo" reference.
func plainHTTPRepo(t *testing.T, srv *httptest.Server) *remote.Repository {
	t.Helper()
	repo, err := remote.NewRepository(srv.Listener.Addr().String() + "/test-repo")
	require.NoError(t, err)
	repo.PlainHTTP = true
	repo.Client = &http.Client{}
	return repo
}

func TestRepository_Untag_SuccessInvalidatesCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	inner := plainHTTPRepo(t, srv)
	rc := newTestRefCache(t, Options{})
	repo := &Repository{Repository: inner, ReferenceCache: rc}

	ref := registry.Reference{
		Registry:   inner.Reference.Registry,
		Repository: inner.Reference.Repository,
		Reference:  "latest",
	}

	desc := ociImageSpecV1.Descriptor{Digest: digest.FromBytes([]byte("x")), Size: 1}
	rc.Add(ref, desc)
	_, ok := rc.Lookup(ref)
	require.True(t, ok)

	require.NoError(t, repo.Untag(t.Context(), "latest"))
	_, ok = rc.Lookup(ref)
	assert.False(t, ok, "successful Untag must invalidate the cached mapping")
}

func TestRepository_Untag_FailureKeepsCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	t.Cleanup(srv.Close)

	inner := plainHTTPRepo(t, srv)
	rc := newTestRefCache(t, Options{})
	repo := &Repository{Repository: inner, ReferenceCache: rc}

	ref := registry.Reference{
		Registry:   inner.Reference.Registry,
		Repository: inner.Reference.Repository,
		Reference:  "latest",
	}

	desc := ociImageSpecV1.Descriptor{Digest: digest.FromBytes([]byte("x")), Size: 1}
	rc.Add(ref, desc)

	require.Error(t, repo.Untag(t.Context(), "latest"))
	_, ok := rc.Lookup(ref)
	assert.True(t, ok, "a failed Untag must not invalidate the cached mapping")
}

func TestRepository_Untag_NilReferenceCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	// Bare struct literal: store is nil (only ProxyRepository sets it). Untag must
	// fall back to a monolithic store over the embedded repository, not panic.
	repo := &Repository{Repository: plainHTTPRepo(t, srv)}
	require.NotNil(t, repo.getStore())
	require.NoError(t, repo.Untag(t.Context(), "latest"))
}

func TestRepository_Tag_SuccessRefreshesCache(t *testing.T) {
	manifest := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json"}`)
	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes(manifest),
		Size:      int64(len(manifest)),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/test-repo/manifests/"+desc.Digest.String():
			w.Header().Set("Content-Type", desc.MediaType)
			w.Header().Set("Docker-Content-Digest", desc.Digest.String())
			_, _ = w.Write(manifest)
		case r.Method == http.MethodPut && r.URL.Path == "/v2/test-repo/manifests/latest":
			w.Header().Set("Docker-Content-Digest", desc.Digest.String())
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	inner := plainHTTPRepo(t, srv)
	rc := newTestRefCache(t, Options{})
	repo := &Repository{Repository: inner, ReferenceCache: rc}

	ref := registry.Reference{
		Registry:   inner.Reference.Registry,
		Repository: inner.Reference.Repository,
		Reference:  "latest",
	}

	require.NoError(t, repo.Tag(t.Context(), desc, "latest"))
	got, ok := rc.Lookup(ref)
	require.True(t, ok, "successful Tag must populate the reference cache")
	assert.Equal(t, desc, got)
}

func TestRepository_Tag_FailureLeavesCacheEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	inner := plainHTTPRepo(t, srv)
	rc := newTestRefCache(t, Options{})
	repo := &Repository{Repository: inner, ReferenceCache: rc}

	ref := registry.Reference{
		Registry:   inner.Reference.Registry,
		Repository: inner.Reference.Repository,
		Reference:  "latest",
	}

	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes([]byte("x")),
		Size:      1,
	}
	require.Error(t, repo.Tag(t.Context(), desc, "latest"))
	_, ok := rc.Lookup(ref)
	assert.False(t, ok, "a failed Tag must not populate the reference cache")
}
