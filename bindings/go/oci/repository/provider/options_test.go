package provider

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci/cache"
	ocicachev1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/config/cache/v1alpha1"
)

func TestWithOCICacheConfig(t *testing.T) {
	always := &ocicachev1alpha1.Config{RemotePolicy: ocicachev1alpha1.RemotePolicyAlways}

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
			name:     "empty policy keeps the options",
			opts:     []Option{WithBlobCacheOptions(&cache.Options{RemotePolicy: cache.RemotePolicyAlways}), WithOCICacheConfig(&ocicachev1alpha1.Config{})},
			wantBlob: &cache.Options{RemotePolicy: cache.RemotePolicyAlways},
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

	t.Run("shared options are not mutated", func(t *testing.T) {
		r := require.New(t)
		shared := &cache.Options{}
		NewComponentVersionRepositoryProvider(WithBlobCacheOptions(shared), WithOCICacheConfig(always))
		r.Empty(shared.RemotePolicy)
	})
}
