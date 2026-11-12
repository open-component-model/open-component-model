package provider

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci/cache"
	ocicachev1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/config/cache/v1alpha1"
)

func TestWithOCICacheConfig(t *testing.T) {
	always := &ocicachev1alpha1.Config{RemotePolicy: ocicachev1alpha1.RemotePolicyAlways}
	all := &ocicachev1alpha1.Config{
		RemotePolicy: ocicachev1alpha1.RemotePolicyAlways,
		TTL:          ocicachev1alpha1.NewDuration(time.Hour),
		MaxBlobSize:  new(int64(1024)),
		MaxTotalSize: new(int64(4096)),
	}

	tests := []struct {
		name     string
		opts     []Option
		wantBlob *cache.Options
		wantRef  *cache.Options
	}{
		{
			name:     "policy applied to both caches",
			opts:     []Option{WithBlobCacheOptions(&cache.Options{}), WithReferenceCacheOptions(&cache.Options{}), WithOCICacheConfig(always)},
			wantBlob: &cache.Options{RemotePolicy: cache.RemotePolicyAlways},
			wantRef:  &cache.Options{RemotePolicy: cache.RemotePolicyAlways},
		},
		{
			name:     "all fields applied to both caches",
			opts:     []Option{WithBlobCacheOptions(&cache.Options{}), WithReferenceCacheOptions(&cache.Options{}), WithOCICacheConfig(all)},
			wantBlob: &cache.Options{RemotePolicy: cache.RemotePolicyAlways, TTL: time.Hour, MaxBlobSize: 1024, MaxTotalSize: 4096},
			wantRef:  &cache.Options{RemotePolicy: cache.RemotePolicyAlways, TTL: time.Hour, MaxBlobSize: 1024, MaxTotalSize: 4096},
		},
		{
			name: "unset fields keep the options",
			opts: []Option{
				WithBlobCacheOptions(&cache.Options{RemotePolicy: cache.RemotePolicyAlways, TTL: time.Minute, MaxBlobSize: 1}),
				WithOCICacheConfig(&ocicachev1alpha1.Config{}),
			},
			wantBlob: &cache.Options{RemotePolicy: cache.RemotePolicyAlways, TTL: time.Minute, MaxBlobSize: 1},
			wantRef:  nil,
		},
		{
			name:     "empty policy keeps the options",
			opts:     []Option{WithBlobCacheOptions(&cache.Options{RemotePolicy: cache.RemotePolicyAlways}), WithOCICacheConfig(&ocicachev1alpha1.Config{})},
			wantBlob: &cache.Options{RemotePolicy: cache.RemotePolicyAlways},
			wantRef:  nil,
		},
		{
			name:     "enabled false disables both caches",
			opts:     []Option{WithBlobCacheOptions(&cache.Options{}), WithReferenceCacheOptions(&cache.Options{}), WithOCICacheConfig(&ocicachev1alpha1.Config{Enabled: new(false)})},
			wantBlob: nil,
			wantRef:  nil,
		},
		{
			name:     "config does not enable disabled caches",
			opts:     []Option{WithOCICacheConfig(always)},
			wantBlob: nil,
			wantRef:  nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			prov := NewComponentVersionRepositoryProvider(tc.opts...)
			r.Equal(tc.wantBlob, prov.blobCacheOpts)
			r.Equal(tc.wantRef, prov.referenceCacheOpts)
		})
	}

	t.Run("enabled false drops a shared blob cache", func(t *testing.T) {
		r := require.New(t)
		bc, err := cache.NewBlobCache(cache.Options{Dir: t.TempDir()})
		r.NoError(err)
		prov := NewComponentVersionRepositoryProvider(WithBlobCache(bc), WithOCICacheConfig(&ocicachev1alpha1.Config{Enabled: new(false)}))
		r.Nil(prov.sharedBlobCache())
	})

	t.Run("shared options are not mutated", func(t *testing.T) {
		r := require.New(t)
		shared := &cache.Options{}
		NewComponentVersionRepositoryProvider(WithBlobCacheOptions(shared), WithReferenceCacheOptions(shared), WithOCICacheConfig(all))
		r.Equal(&cache.Options{}, shared)
	})
}
