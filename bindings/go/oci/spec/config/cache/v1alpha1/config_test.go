package v1alpha1_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	cachespec "ocm.software/open-component-model/bindings/go/oci/spec/config/cache/v1alpha1"
)

func TestLookupConfig(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		expect    cachespec.RemotePolicy
		expectNil bool
		wantErr   string
	}{
		{
			name: "returns nil without an entry",
			yaml: `
type: generic.config.ocm.software/v1
configurations: []
`,
			expectNil: true,
		},
		{
			name: "leaves the policy empty when the field is omitted",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.cache.config.ocm.software/v1alpha1
`,
			expect: "",
		},
		{
			name: "parses Always",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.cache.config.ocm.software/v1alpha1
    remotePolicy: Always
`,
			expect: cachespec.RemotePolicyAlways,
		},
		{
			name: "accepts the unversioned type",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.cache.config.ocm.software
    remotePolicy: Always
`,
			expect: cachespec.RemotePolicyAlways,
		},
		{
			name: "last entry wins",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.cache.config.ocm.software/v1alpha1
    remotePolicy: Always
  - type: oci.cache.config.ocm.software/v1alpha1
    remotePolicy: IfNotPresent
`,
			expect: cachespec.RemotePolicyIfNotPresent,
		},
		{
			name: "an entry without the field keeps the earlier value",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.cache.config.ocm.software/v1alpha1
    remotePolicy: Always
  - type: oci.cache.config.ocm.software/v1alpha1
`,
			expect: cachespec.RemotePolicyAlways,
		},
		{
			name: "rejects an unknown policy",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.cache.config.ocm.software/v1alpha1
    remotePolicy: always
`,
			wantErr: `unknown remotePolicy "always"`,
		},
		{
			name: "rejects an unknown policy that a later entry overrides",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.cache.config.ocm.software/v1alpha1
    remotePolicy: Never
  - type: oci.cache.config.ocm.software/v1alpha1
    remotePolicy: Always
`,
			wantErr: `unknown remotePolicy "Never"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			var generic genericv1.Config
			r.NoError(genericv1.Scheme.Decode(strings.NewReader(tt.yaml), &generic))

			cfg, err := cachespec.LookupConfig(&generic)
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				return
			}
			r.NoError(err)
			if tt.expectNil {
				r.Nil(cfg)
				return
			}
			r.NotNil(cfg)
			r.Equal(tt.expect, cfg.RemotePolicy)
		})
	}

	t.Run("nil config returns nil", func(t *testing.T) {
		r := require.New(t)
		cfg, err := cachespec.LookupConfig(nil)
		r.NoError(err)
		r.Nil(cfg)
	})
}
