package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	ocmv1 "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/spec"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestResolveOwnershipReferrerPolicy(t *testing.T) {
	cfgType := runtime.NewVersionedType(ocmv1.ConfigType, ocmv1.Version)
	makeCfg := func(t *testing.T, policy ocmv1.OwnershipReferrerPolicy) *genericv1.Config {
		t.Helper()
		specCfg := &ocmv1.Config{Type: cfgType, OwnershipReferrerPolicy: policy}
		raw := &runtime.Raw{}
		require.NoError(t, ocmv1.Scheme.Convert(specCfg, raw))
		return &genericv1.Config{
			Type:           runtime.NewVersionedType(genericv1.ConfigType, genericv1.Version),
			Configurations: []*runtime.Raw{raw},
		}
	}

	tests := []struct {
		name    string
		cfg     *genericv1.Config
		want    oci.OwnershipReferrerPolicy
		wantErr bool
	}{
		{name: "nil config maps to disabled", cfg: nil, want: oci.OwnershipReferrerPolicyDisabled},
		{name: "empty config maps to disabled", cfg: &genericv1.Config{}, want: oci.OwnershipReferrerPolicyDisabled},
		{name: "Auto string maps to Auto", cfg: makeCfg(t, ocmv1.OwnershipReferrerPolicyAuto), want: oci.OwnershipReferrerPolicyAuto},
		{name: "Disabled string maps to disabled", cfg: makeCfg(t, ocmv1.OwnershipReferrerPolicyDisabled), want: oci.OwnershipReferrerPolicyDisabled},
		{name: "unsupported string returns error", cfg: makeCfg(t, "Bogus"), wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveOwnershipReferrerPolicy(tc.cfg)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
