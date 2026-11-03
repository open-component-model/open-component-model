package oci

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci/cache"
	ocicachev1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/config/cache/v1alpha1"
)

func TestRemotePolicyFromConfig(t *testing.T) {
	tests := []struct {
		name   string
		cfg    *ocicachev1alpha1.Config
		expect cache.RemotePolicy
	}{
		{name: "nil config", cfg: nil, expect: cache.RemotePolicyIfNotPresent},
		{name: "empty policy", cfg: &ocicachev1alpha1.Config{}, expect: cache.RemotePolicyIfNotPresent},
		{name: "IfNotPresent", cfg: &ocicachev1alpha1.Config{RemotePolicy: ocicachev1alpha1.RemotePolicyIfNotPresent}, expect: cache.RemotePolicyIfNotPresent},
		{name: "Always", cfg: &ocicachev1alpha1.Config{RemotePolicy: ocicachev1alpha1.RemotePolicyAlways}, expect: cache.RemotePolicyAlways},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			r.Equal(tt.expect, remotePolicyFromConfig(tt.cfg))
		})
	}
}
