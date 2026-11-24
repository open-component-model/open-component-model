package v1alpha1_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	storagespec "ocm.software/open-component-model/bindings/go/oci/spec/config/storage/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
)

func TestLookupConfig(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		expect    layout.ComponentVersionLayout
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
			name: "leaves the layout empty when the field is omitted",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.storage.config.ocm.software/v1alpha1
`,
			expect: "",
		},
		{
			name: "parses normalized",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.storage.config.ocm.software/v1alpha1
    componentVersionLayout: normalized
`,
			expect: layout.ComponentVersionLayoutNormalized,
		},
		{
			name: "accepts the unversioned type",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.storage.config.ocm.software
    componentVersionLayout: normalized
`,
			expect: layout.ComponentVersionLayoutNormalized,
		},
		{
			name: "last entry wins",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.storage.config.ocm.software/v1alpha1
    componentVersionLayout: normalized
  - type: oci.storage.config.ocm.software/v1alpha1
    componentVersionLayout: v2
`,
			expect: layout.ComponentVersionLayoutV2,
		},
		{
			name: "an entry without the field keeps the earlier value",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.storage.config.ocm.software/v1alpha1
    componentVersionLayout: normalized
  - type: oci.storage.config.ocm.software/v1alpha1
`,
			expect: layout.ComponentVersionLayoutNormalized,
		},
		{
			name: "rejects an unknown layout",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.storage.config.ocm.software/v1alpha1
    componentVersionLayout: v3
`,
			wantErr: `unknown component version layout "v3"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			var generic genericv1.Config
			r.NoError(genericv1.Scheme.Decode(strings.NewReader(tt.yaml), &generic))

			cfg, err := storagespec.LookupConfig(&generic)
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
			r.Equal(tt.expect, cfg.ComponentVersionLayout)
		})
	}

	t.Run("nil config returns nil", func(t *testing.T) {
		r := require.New(t)
		cfg, err := storagespec.LookupConfig(nil)
		r.NoError(err)
		r.Nil(cfg)
	})
}
