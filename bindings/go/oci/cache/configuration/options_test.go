package configuration_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci/cache"
	cacheconfiguration "ocm.software/open-component-model/bindings/go/oci/cache/configuration"
	ocicachingv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/config/v1alpha1"
)

func TestResolve(t *testing.T) {
	size := int64(1024)
	ttl := ocicachingv1alpha1.NewDuration(30 * time.Second)

	tests := []struct {
		name     string
		cfg      *ocicachingv1alpha1.Config
		fallback cache.RemotePolicy
		want     cache.RemotePolicy
	}{
		{"nil + cli fallback", nil, cache.RemotePolicyIfNotPresent, cache.RemotePolicyIfNotPresent},
		{"nil + Always fallback", nil, cache.RemotePolicyAlways, cache.RemotePolicyAlways},
		{"omitted mode uses fallback", &ocicachingv1alpha1.Config{}, cache.RemotePolicyAlways, cache.RemotePolicyAlways},
		{"Always overrides fallback", &ocicachingv1alpha1.Config{Mode: ocicachingv1alpha1.ModeAlways}, cache.RemotePolicyIfNotPresent, cache.RemotePolicyAlways},
		{"IfNotPresent overrides fallback", &ocicachingv1alpha1.Config{Mode: ocicachingv1alpha1.ModeIfNotPresent}, cache.RemotePolicyAlways, cache.RemotePolicyIfNotPresent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			blob, ref, err := cacheconfiguration.Resolve(tt.cfg, tt.fallback)
			r.NoError(err)
			r.NotNil(blob)
			r.NotNil(ref)
			r.NotSame(blob, ref)
			r.Equal(tt.want, blob.RemotePolicy)
			r.Equal(tt.want, ref.RemotePolicy)
			r.Empty(blob.Dir)
			r.Empty(ref.Dir)
		})
	}

	t.Run("Never disables both", func(t *testing.T) {
		r := require.New(t)
		blob, ref, err := cacheconfiguration.Resolve(&ocicachingv1alpha1.Config{Mode: ocicachingv1alpha1.ModeNever}, cache.RemotePolicyAlways)
		r.NoError(err)
		r.Nil(blob)
		r.Nil(ref)
	})

	t.Run("ttl and maxBlobSize", func(t *testing.T) {
		r := require.New(t)
		d := cache.Defaults()
		blob, ref, err := cacheconfiguration.Resolve(&ocicachingv1alpha1.Config{TTL: ttl, MaxBlobSize: &size}, cache.RemotePolicyAlways)
		r.NoError(err)
		r.Equal(30*time.Second, blob.TTL)
		r.Equal(30*time.Second, ref.TTL)
		r.Equal(size, blob.MaxBlobSize)
		r.Equal(d.MaxBlobSize, ref.MaxBlobSize)
		r.Equal(d.MaxEntries, blob.MaxEntries)
		r.Equal(d.MaxEntries, ref.MaxEntries)
	})

	t.Run("defaults and admission filter preserved", func(t *testing.T) {
		r := require.New(t)
		d := cache.Defaults()
		blob, ref, err := cacheconfiguration.Resolve(nil, cache.RemotePolicyAlways)
		r.NoError(err)
		r.Equal(d.TTL, blob.TTL)
		r.Equal(d.TTL, ref.TTL)
		r.Equal(d.MaxBlobSize, blob.MaxBlobSize)
		r.NotNil(blob.Accept)
	})

	t.Run("invalid fallback", func(t *testing.T) {
		for _, fb := range []cache.RemotePolicy{"", "Never", "bogus"} {
			_, _, err := cacheconfiguration.Resolve(nil, fb)
			require.Error(t, err)
		}
	})

	t.Run("invalid config", func(t *testing.T) {
		zero := int64(0)
		neg := ocicachingv1alpha1.NewDuration(-time.Second)
		for _, cfg := range []*ocicachingv1alpha1.Config{
			{Mode: "bogus"},
			{MaxBlobSize: &zero},
			{TTL: neg},
			{TTL: ocicachingv1alpha1.NewDuration(0)},
		} {
			_, _, err := cacheconfiguration.Resolve(cfg, cache.RemotePolicyAlways)
			require.Error(t, err)
		}
	})
}
