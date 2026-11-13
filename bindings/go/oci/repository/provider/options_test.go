package provider

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	typesv1 "ocm.software/open-component-model/bindings/go/configuration/types/v1"
	"ocm.software/open-component-model/bindings/go/oci/cache"
	ocicachev1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/config/cache/v1alpha1"
)

func TestWithOCICacheConfig(t *testing.T) {
	always := &ocicachev1alpha1.Config{RemotePolicy: ocicachev1alpha1.RemotePolicyAlways}
	all := &ocicachev1alpha1.Config{
		RemotePolicy: ocicachev1alpha1.RemotePolicyIfNotPresent,
		Blob: &ocicachev1alpha1.BlobCacheConfig{
			TTL:          typesv1.NewDuration(time.Hour),
			MaxBlobSize:  new(int64(1024)),
			MaxTotalSize: new(int64(4096)),
		},
		Reference: &ocicachev1alpha1.ReferenceCacheConfig{TTL: typesv1.NewDuration(time.Minute), MaxEntries: new(500)},
	}
	enabled := func(blob, ref bool) *ocicachev1alpha1.Config {
		return &ocicachev1alpha1.Config{
			Blob:      &ocicachev1alpha1.BlobCacheConfig{Enabled: new(blob)},
			Reference: &ocicachev1alpha1.ReferenceCacheConfig{Enabled: new(ref)},
		}
	}
	bothCaches := []Option{WithBlobCacheOptions(&cache.Options{}), WithReferenceCacheOptions(&cache.Options{})}

	tests := []struct {
		name     string
		opts     []Option
		wantBlob *cache.Options
		wantRef  *cache.Options
	}{
		{
			name:     "Always leaves no reference cache",
			opts:     append(bothCaches, WithOCICacheConfig(always)),
			wantBlob: &cache.Options{RemotePolicy: cache.RemotePolicyAlways},
		},
		{
			name: "Always leaves no reference cache even if enabled",
			opts: append(bothCaches, WithOCICacheConfig(&ocicachev1alpha1.Config{
				RemotePolicy: ocicachev1alpha1.RemotePolicyAlways,
				Reference:    &ocicachev1alpha1.ReferenceCacheConfig{Enabled: new(true)},
			})),
			wantBlob: &cache.Options{RemotePolicy: cache.RemotePolicyAlways},
		},
		{
			name:     "each section applies to its cache",
			opts:     append(bothCaches, WithOCICacheConfig(all)),
			wantBlob: &cache.Options{RemotePolicy: cache.RemotePolicyIfNotPresent, TTL: time.Hour, MaxBlobSize: 1024, MaxTotalSize: 4096},
			wantRef:  &cache.Options{RemotePolicy: cache.RemotePolicyIfNotPresent, TTL: time.Minute, MaxEntries: 500},
		},
		{
			name: "unset fields keep the options",
			opts: []Option{
				WithBlobCacheOptions(&cache.Options{RemotePolicy: cache.RemotePolicyAlways, TTL: time.Minute, MaxBlobSize: 1}),
				WithOCICacheConfig(&ocicachev1alpha1.Config{}),
			},
			wantBlob: &cache.Options{RemotePolicy: cache.RemotePolicyAlways, TTL: time.Minute, MaxBlobSize: 1},
		},
		{
			name:     "no config keeps the options",
			opts:     bothCaches,
			wantBlob: &cache.Options{},
			wantRef:  &cache.Options{},
		},
		{
			name:     "a cache without options stays off",
			opts:     []Option{WithOCICacheConfig(all)},
			wantBlob: nil,
			wantRef:  nil,
		},
		{
			name: "enabled false turns each cache off",
			opts: append(bothCaches, WithOCICacheConfig(enabled(false, false))),
		},
		{
			name:    "enabled turns the caches off independently",
			opts:    append(bothCaches, WithOCICacheConfig(enabled(false, true))),
			wantRef: &cache.Options{},
		},
		{
			name:     "enabled true turns a cache without options on",
			opts:     []Option{WithOCICacheConfig(enabled(true, true))},
			wantBlob: &cache.Options{},
			wantRef:  &cache.Options{},
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

	t.Run("blob enabled false drops a shared blob cache", func(t *testing.T) {
		r := require.New(t)
		bc, err := cache.NewBlobCache(cache.Options{Dir: t.TempDir()})
		r.NoError(err)
		prov := NewComponentVersionRepositoryProvider(WithBlobCache(bc), WithOCICacheConfig(enabled(false, true)))
		r.Nil(prov.sharedBlobCache())
	})

	t.Run("a shared reference cache is used unless turned off", func(t *testing.T) {
		r := require.New(t)
		rc, err := cache.NewReferenceCache(cache.Options{Dir: t.TempDir()})
		r.NoError(err)
		r.Same(rc, NewComponentVersionRepositoryProvider(WithReferenceCache(rc)).sharedReferenceCache())
		r.Nil(NewComponentVersionRepositoryProvider(WithReferenceCache(rc), WithOCICacheConfig(enabled(true, false))).sharedReferenceCache())
		r.Nil(NewComponentVersionRepositoryProvider(WithReferenceCache(rc), WithOCICacheConfig(always)).sharedReferenceCache())
	})

	t.Run("no reference cache for Always options", func(t *testing.T) {
		prov := NewComponentVersionRepositoryProvider(WithReferenceCacheOptions(&cache.Options{RemotePolicy: cache.RemotePolicyAlways}))
		require.Nil(t, prov.sharedReferenceCache())
	})

	t.Run("one reference cache serves every repository", func(t *testing.T) {
		r := require.New(t)
		dir := t.TempDir()
		prov := NewComponentVersionRepositoryProvider(WithTempDir(dir), WithReferenceCacheOptions(&cache.Options{}))
		first := prov.sharedReferenceCache()
		r.NotNil(first)
		r.Same(first, prov.sharedReferenceCache())
		r.DirExists(filepath.Join(dir, "ocm-oci-refcache", "refs"))
	})

	t.Run("shared options are not mutated", func(t *testing.T) {
		r := require.New(t)
		shared := &cache.Options{}
		NewComponentVersionRepositoryProvider(WithBlobCacheOptions(shared), WithReferenceCacheOptions(shared), WithOCICacheConfig(all))
		r.Equal(&cache.Options{}, shared)
	})
}
