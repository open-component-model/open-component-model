package provider

import (
	"testing"

	"github.com/stretchr/testify/require"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci/internal/remotestore"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/repository"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ocirepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
)

// resolveChunkSettings builds a resolver for baseURL the way the provider does
// (NewResolver default-on plus the computed per-host/global chunked-push
// override), then inspects the RemoteStore the resolver hands out so the test
// asserts the effective ChunkSize/ChunkThreshold rather than resolver internals.
func resolveChunkSettings(t *testing.T, cfg *httpv1alpha1.Config, baseURL string) (int64, int64) {
	t.Helper()
	r := require.New(t)
	obj := &ocirepospecv1.Repository{BaseUrl: baseURL}

	var extra []urlresolver.Option
	if opt, ok := chunkedPushResolverOption(cfg, ociRepositoryHost(obj)); ok {
		extra = append(extra, opt)
	}

	resolver, err := ocirepository.NewResolver(t.Context(), nil, obj, extra...)
	r.NoError(err)

	store, err := resolver.StoreForReference(t.Context(), baseURL+"/some/component:v1.0.0")
	r.NoError(err)

	rs, ok := store.(*remotestore.RemoteStore)
	r.True(ok, "expected *remotestore.RemoteStore, got %T", store)
	return rs.ChunkSize, rs.ChunkThreshold
}

func TestChunkedPushResolverOption(t *testing.T) {
	t.Run("nil config leaves default-on", func(t *testing.T) {
		r := require.New(t)
		size, threshold := resolveChunkSettings(t, nil, "registry.example.com")
		r.Equal(remotestore.DefaultChunkSize, size)
		r.Equal(remotestore.DefaultChunkThreshold, threshold)
	})

	t.Run("unset config leaves default-on", func(t *testing.T) {
		r := require.New(t)
		size, threshold := resolveChunkSettings(t, &httpv1alpha1.Config{}, "registry.example.com")
		r.Equal(remotestore.DefaultChunkSize, size)
		r.Equal(remotestore.DefaultChunkThreshold, threshold)
	})

	t.Run("global disabled turns chunking off", func(t *testing.T) {
		r := require.New(t)
		cfg := &httpv1alpha1.Config{ChunkedPush: &httpv1alpha1.ChunkedPushConfig{Disabled: true}}
		size, _ := resolveChunkSettings(t, cfg, "registry.example.com")
		// ChunkSize <= 0 means monolithic push.
		r.Equal(int64(0), size)
	})

	t.Run("global custom sizes applied", func(t *testing.T) {
		r := require.New(t)
		cfg := &httpv1alpha1.Config{ChunkedPush: &httpv1alpha1.ChunkedPushConfig{
			ChunkSize: 1 << 20,
			Threshold: 1 << 21,
		}}
		size, threshold := resolveChunkSettings(t, cfg, "registry.example.com")
		r.Equal(int64(1<<20), size)
		r.Equal(int64(1<<21), threshold)
	})

	t.Run("global set with zero sizes maps to defaults", func(t *testing.T) {
		r := require.New(t)
		cfg := &httpv1alpha1.Config{ChunkedPush: &httpv1alpha1.ChunkedPushConfig{}}
		size, threshold := resolveChunkSettings(t, cfg, "registry.example.com")
		r.Equal(remotestore.DefaultChunkSize, size)
		r.Equal(remotestore.DefaultChunkThreshold, threshold)
	})

	t.Run("per-host disabled overrides global enabled", func(t *testing.T) {
		r := require.New(t)
		cfg := &httpv1alpha1.Config{
			ChunkedPush: &httpv1alpha1.ChunkedPushConfig{ChunkSize: 1 << 20},
			Hosts: map[string]*httpv1alpha1.HostConfig{
				"registry.example.com": {ChunkedPush: &httpv1alpha1.ChunkedPushConfig{Disabled: true}},
			},
		}
		size, _ := resolveChunkSettings(t, cfg, "registry.example.com")
		r.Equal(int64(0), size)
	})

	t.Run("per-host sizes tune only that host; others fall back to global", func(t *testing.T) {
		r := require.New(t)
		cfg := &httpv1alpha1.Config{
			ChunkedPush: &httpv1alpha1.ChunkedPushConfig{ChunkSize: 1 << 20, Threshold: 1 << 20},
			Hosts: map[string]*httpv1alpha1.HostConfig{
				"tuned.example.com": {ChunkedPush: &httpv1alpha1.ChunkedPushConfig{
					ChunkSize: 8 << 20,
					Threshold: 32 << 20,
				}},
			},
		}

		tunedSize, tunedThreshold := resolveChunkSettings(t, cfg, "tuned.example.com")
		r.Equal(int64(8<<20), tunedSize)
		r.Equal(int64(32<<20), tunedThreshold)

		otherSize, otherThreshold := resolveChunkSettings(t, cfg, "other.example.com")
		r.Equal(int64(1<<20), otherSize)
		r.Equal(int64(1<<20), otherThreshold)
	})

	t.Run("host matching mirrors HTTP client: bare host entry covers a port", func(t *testing.T) {
		r := require.New(t)
		cfg := &httpv1alpha1.Config{
			Hosts: map[string]*httpv1alpha1.HostConfig{
				"registry.example.com": {ChunkedPush: &httpv1alpha1.ChunkedPushConfig{Disabled: true}},
			},
		}
		size, _ := resolveChunkSettings(t, cfg, "registry.example.com:5000")
		r.Equal(int64(0), size)
	})
}

func TestOCIRepositoryHost(t *testing.T) {
	r := require.New(t)
	r.Equal("registry.example.com", ociRepositoryHost(&ocirepospecv1.Repository{BaseUrl: "https://registry.example.com/v2"}))
	r.Equal("registry.example.com:5000", ociRepositoryHost(&ocirepospecv1.Repository{BaseUrl: "registry.example.com:5000/sub"}))
	r.Equal("registry.example.com", ociRepositoryHost(&ocirepospecv1.Repository{BaseUrl: "http://registry.example.com/"}))
}
