package cache

import (
	"bytes"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"
)

func TestRepository_Fetch_DelegatesToBlobCache(t *testing.T) {
	c := newTestCache(t, Options{})
	manifest := []byte(`{"schemaVersion":2}`)
	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes(manifest),
		Size:      int64(len(manifest)),
	}
	_, err := c.Populate(t.Context(), desc.Digest, desc.Size, bytes.NewReader(manifest))
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
	inner := &remote.Repository{
		Reference: registry.Reference{Registry: "ghcr.io", Repository: "owner/repo"},
	}
	rc.Add("ghcr.io/owner/repo", "ref:1", desc)

	repo := &Repository{Repository: inner, ReferenceCache: rc}
	got, err := repo.Resolve(t.Context(), "ref:1")
	require.NoError(t, err)
	assert.Equal(t, desc, got)
}

func TestRepository_referenceNamespace(t *testing.T) {
	tests := []struct {
		name string
		repo *remote.Repository
		want string
	}{
		{"nil repo", nil, ""},
		{"empty reference", &remote.Repository{}, ""},
		{
			"only registry",
			&remote.Repository{Reference: registry.Reference{Registry: "ghcr.io"}},
			"",
		},
		{
			"fully qualified",
			&remote.Repository{Reference: registry.Reference{Registry: "ghcr.io", Repository: "owner/repo"}},
			"ghcr.io/owner/repo",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &Repository{Repository: tc.repo}
			assert.Equal(t, tc.want, r.referenceNamespace())
		})
	}
}

func TestRepository_Unwrap_ReturnsEmbedded(t *testing.T) {
	inner := &remote.Repository{}
	repo := &Repository{Repository: inner, BlobCache: newTestCache(t, Options{})}
	assert.Same(t, inner, repo.Unwrap())
}

func TestProxyRepository_NilCachesReturnBase(t *testing.T) {
	inner := &remote.Repository{}
	got := ProxyRepository(inner, nil, nil)
	assert.Same(t, inner, got)
}

func TestProxyRepository_BlobCacheOnlyWraps(t *testing.T) {
	inner := &remote.Repository{}
	c := newTestCache(t, Options{})
	got := ProxyRepository(inner, c, nil)
	wrapped, ok := got.(*Repository)
	require.True(t, ok)
	assert.Same(t, inner, wrapped.Repository)
	assert.Same(t, c, wrapped.BlobCache)
	assert.Nil(t, wrapped.ReferenceCache)
}

func TestProxyRepository_ReferenceCacheOnlyWraps(t *testing.T) {
	inner := &remote.Repository{}
	rc := newTestRefCache(t, Options{})
	got := ProxyRepository(inner, nil, rc)
	wrapped, ok := got.(*Repository)
	require.True(t, ok)
	assert.Same(t, inner, wrapped.Repository)
	assert.Nil(t, wrapped.BlobCache)
	assert.Same(t, rc, wrapped.ReferenceCache)
}

func TestProxyRepository_BothCachesWrap(t *testing.T) {
	inner := &remote.Repository{}
	c := newTestCache(t, Options{})
	rc := newTestRefCache(t, Options{})
	got := ProxyRepository(inner, c, rc)
	wrapped, ok := got.(*Repository)
	require.True(t, ok)
	assert.Same(t, inner, wrapped.Repository)
	assert.Same(t, c, wrapped.BlobCache)
	assert.Same(t, rc, wrapped.ReferenceCache)
}
