package spec_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

func TestConfig_ParseYAML(t *testing.T) {
	tests := []struct {
		name          string
		yaml          string
		wantRecursive spec.Recursive
	}{
		{
			name: "recursive field",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    recursive: -1
`,
			wantRecursive: spec.RecursiveInfinite,
		},
		{
			name: "fields omitted stay empty",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
`,
			wantRecursive: spec.RecursiveNone,
		},
		{
			name: "unversioned type alias",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var generic genericv1.Config
			err := genericv1.Scheme.Decode(strings.NewReader(tt.yaml), &generic)
			require.NoError(t, err)
			require.Len(t, generic.Configurations, 1)

			var cfg spec.Config
			err = spec.Scheme.Convert(generic.Configurations[0], &cfg)
			require.NoError(t, err)

			assert.Equal(t, tt.wantRecursive, cfg.Recursive)
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     spec.Config
		wantErr string
	}{
		{"valid empty", spec.Config{}, ""},
		{"valid recursive infinite", spec.Config{Recursive: spec.RecursiveInfinite}, ""},
		{"valid recursive none", spec.Config{Recursive: spec.RecursiveNone}, ""},
		{"recursive depth not implemented", spec.Config{Recursive: 3}, "not implemented"},
		{"invalid recursive below -1", spec.Config{Recursive: -5}, "invalid recursive"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantErr)
			}
		})
	}
}

func TestMerge(t *testing.T) {
	t.Run("empty returns nil", func(t *testing.T) {
		assert.Nil(t, spec.Merge())
	})

	t.Run("later non-empty fields win", func(t *testing.T) {
		a := &spec.Config{Recursive: spec.RecursiveInfinite}
		b := &spec.Config{Recursive: spec.RecursiveNone}

		merged := spec.Merge(a, b)

		assert.Equal(t, spec.RecursiveInfinite, merged.Recursive)
	})

	t.Run("nil element is skipped", func(t *testing.T) {
		a := &spec.Config{Recursive: spec.RecursiveInfinite}

		merged := spec.Merge(nil, a, nil)

		assert.Equal(t, spec.RecursiveInfinite, merged.Recursive)
	})
}

func TestLookupConfig(t *testing.T) {
	decode := func(t *testing.T, yaml string) *genericv1.Config {
		t.Helper()
		var generic genericv1.Config
		require.NoError(t, genericv1.Scheme.Decode(strings.NewReader(yaml), &generic))
		return &generic
	}

	t.Run("no transfer entries", func(t *testing.T) {
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: other.config.ocm.software/v1
`)
		cfg, err := spec.LookupConfig(generic)
		require.NoError(t, err)
		assert.Nil(t, cfg)
	})

	t.Run("single entry", func(t *testing.T) {
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    recursive: -1
`)
		cfg, err := spec.LookupConfig(generic)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, spec.RecursiveInfinite, cfg.Recursive)
	})

	t.Run("later entry wins, unset fields fall through", func(t *testing.T) {
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    recursive: -1
  - type: transfer.config.ocm.software/v1alpha1
`)
		cfg, err := spec.LookupConfig(generic)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, spec.RecursiveInfinite, cfg.Recursive)
	})

	t.Run("stale copyMode is rejected", func(t *testing.T) {
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    copyMode: allResources
`)
		_, err := spec.LookupConfig(generic)
		require.ErrorContains(t, err, `unknown field "copyMode"`)
	})

	t.Run("recursive depth is rejected", func(t *testing.T) {
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    recursive: 3
`)
		_, err := spec.LookupConfig(generic)
		require.ErrorContains(t, err, "not implemented")
	})

	t.Run("stale uploadType is rejected", func(t *testing.T) {
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    uploadType: ociArtifact
`)
		_, err := spec.LookupConfig(generic)
		require.ErrorContains(t, err, `unknown field "uploadType"`)
	})
}
