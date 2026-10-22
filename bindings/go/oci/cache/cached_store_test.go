package cache

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"

	"ocm.software/open-component-model/bindings/go/oci/internal/remotestore"
)

func newCachedStore(inner *remote.Repository, blob *BlobCache, refs *ReferenceCache) *CachedStore {
	return &CachedStore{
		RemoteStore:    &remotestore.RemoteStore{Repository: inner},
		BlobCache:      blob,
		ReferenceCache: refs,
	}
}

func TestCachedStore_Fetch_DelegatesToBlobCache(t *testing.T) {
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
	repo := newCachedStore(&remote.Repository{}, c, nil)
	rc, err := repo.Fetch(t.Context(), desc)
	require.NoError(t, err)
	defer rc.Close()
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, manifest, got)
}

func TestCachedStore_Resolve_DelegatesToReferenceCache(t *testing.T) {
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

	repo := newCachedStore(inner, nil, rc)
	got, err := repo.Resolve(t.Context(), ref)
	require.NoError(t, err)
	assert.Equal(t, desc, got)
}

func TestCachedStore_Resolve_WithFullReferenceAndNormalization(t *testing.T) {
	rc := newTestRefCache(t, Options{})
	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes([]byte("normalized-test")),
		Size:      15,
	}
	inner := &remote.Repository{
		Reference: registry.Reference{Registry: "ghcr.io", Repository: "owner/repo"},
	}
	repo := newCachedStore(inner, nil, rc)

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

func TestCachedStore_Unwrap_ReturnsEmbedded(t *testing.T) {
	inner := &remote.Repository{}
	repo := newCachedStore(inner, newTestCache(t, Options{}), nil)
	assert.Same(t, inner, repo.Unwrap())
}

func TestWrap_NilCachesReturnBase(t *testing.T) {
	inner := &remote.Repository{}
	backing := &remotestore.RemoteStore{Repository: inner}
	got := Wrap(backing, nil, nil)
	// All-nil caches: Wrap must hand the backing store through so chunked
	// push and other RemoteStore features survive.
	assert.Same(t, backing, got)
}

func TestWrap_PreservesChunkConfigPassthrough(t *testing.T) {
	// With no caches configured Wrap must not strip chunk configuration.
	inner := &remote.Repository{}
	backing := &remotestore.RemoteStore{Repository: inner, ChunkSize: 4 << 20, ChunkThreshold: 8 << 20}
	got := Wrap(backing, nil, nil)
	rs, ok := got.(*remotestore.RemoteStore)
	require.True(t, ok)
	assert.Equal(t, int64(4<<20), rs.ChunkSize)
	assert.Equal(t, int64(8<<20), rs.ChunkThreshold)
}

func TestWrap_BlobCacheOnlyWraps(t *testing.T) {
	inner := &remote.Repository{}
	c := newTestCache(t, Options{})
	got := Wrap(&remotestore.RemoteStore{Repository: inner}, c, nil)
	wrapped, ok := got.(*CachedStore)
	require.True(t, ok)
	assert.Same(t, inner, wrapped.Repository)
	assert.Same(t, c, wrapped.BlobCache)
	assert.Nil(t, wrapped.ReferenceCache)
}

func TestWrap_ReferenceCacheOnlyWraps(t *testing.T) {
	inner := &remote.Repository{}
	rc := newTestRefCache(t, Options{})
	got := Wrap(&remotestore.RemoteStore{Repository: inner}, nil, rc)
	wrapped, ok := got.(*CachedStore)
	require.True(t, ok)
	assert.Same(t, inner, wrapped.Repository)
	assert.Nil(t, wrapped.BlobCache)
	assert.Same(t, rc, wrapped.ReferenceCache)
}

func TestWrap_BothCachesWrap(t *testing.T) {
	inner := &remote.Repository{}
	c := newTestCache(t, Options{})
	rc := newTestRefCache(t, Options{})
	got := Wrap(&remotestore.RemoteStore{Repository: inner}, c, rc)
	wrapped, ok := got.(*CachedStore)
	require.True(t, ok)
	assert.Same(t, inner, wrapped.Repository)
	assert.Same(t, c, wrapped.BlobCache)
	assert.Same(t, rc, wrapped.ReferenceCache)
}

func TestWrap_PreservesBackingThroughCache(t *testing.T) {
	// The embedded RemoteStore (and its ChunkSize/ChunkThreshold) must
	// survive wrapping so chunked push is not silently disabled when a
	// cache is configured.
	inner := &remote.Repository{}
	backing := &remotestore.RemoteStore{Repository: inner, ChunkSize: 4 << 20, ChunkThreshold: 8 << 20}
	c := newTestCache(t, Options{})
	got := Wrap(backing, c, nil)
	wrapped, ok := got.(*CachedStore)
	require.True(t, ok)
	assert.Same(t, backing, wrapped.RemoteStore)
	assert.Equal(t, int64(4<<20), wrapped.ChunkSize)
	assert.Equal(t, int64(8<<20), wrapped.ChunkThreshold)
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

func TestCachedStore_Untag_SuccessInvalidatesCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	inner := plainHTTPRepo(t, srv)
	rc := newTestRefCache(t, Options{})
	repo := newCachedStore(inner, nil, rc)

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

func TestCachedStore_Untag_FailureKeepsCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	t.Cleanup(srv.Close)

	inner := plainHTTPRepo(t, srv)
	rc := newTestRefCache(t, Options{})
	repo := newCachedStore(inner, nil, rc)

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

func TestCachedStore_Untag_NilReferenceCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	repo := newCachedStore(plainHTTPRepo(t, srv), nil, nil)
	require.NoError(t, repo.Untag(t.Context(), "latest"))
}

func TestCachedStore_Tag_SuccessRefreshesCache(t *testing.T) {
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
	repo := newCachedStore(inner, nil, rc)

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

func TestCachedStore_Tag_FailureLeavesCacheEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	inner := plainHTTPRepo(t, srv)
	rc := newTestRefCache(t, Options{})
	repo := newCachedStore(inner, nil, rc)

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
