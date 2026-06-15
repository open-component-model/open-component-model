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
		name           string
		yaml           string
		wantRecursive  spec.Recursive
		wantCopyMode   spec.CopyMode
		wantUploadType spec.UploadType
	}{
		{
			name: "all fields",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    recursive: -1
    copyMode: allResources
    uploadType: ociArtifact
`,
			wantRecursive:  spec.RecursiveInfinite,
			wantCopyMode:   spec.CopyModeAllResources,
			wantUploadType: spec.UploadAsOciArtifact,
		},
		{
			name: "fields omitted stay empty",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
`,
			wantRecursive:  spec.RecursiveNone,
			wantCopyMode:   "",
			wantUploadType: "",
		},
		{
			name: "unversioned type alias",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software
    copyMode: localBlob
`,
			wantCopyMode: spec.CopyModeLocalBlobResources,
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
			assert.Equal(t, tt.wantCopyMode, cfg.CopyMode)
			assert.Equal(t, tt.wantUploadType, cfg.UploadType)
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
		{"valid all fields", spec.Config{Recursive: spec.RecursiveInfinite, CopyMode: spec.CopyModeAllResources, UploadType: spec.UploadAsOciArtifact}, ""},
		{"valid recursive none", spec.Config{Recursive: spec.RecursiveNone}, ""},
		{"valid copyMode localBlob", spec.Config{CopyMode: spec.CopyModeLocalBlobResources}, ""},
		{"valid uploadType default", spec.Config{UploadType: spec.UploadAsDefault}, ""},
		{"valid uploadType localBlob", spec.Config{UploadType: spec.UploadAsLocalBlob}, ""},
		{"invalid copyMode", spec.Config{CopyMode: "garbage"}, "invalid copyMode"},
		{"invalid uploadType", spec.Config{UploadType: "garbage"}, "invalid uploadType"},
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
		a := &spec.Config{Recursive: spec.RecursiveInfinite, CopyMode: spec.CopyModeLocalBlobResources, UploadType: spec.UploadAsLocalBlob}
		b := &spec.Config{CopyMode: spec.CopyModeAllResources}

		merged := spec.Merge(a, b)

		assert.Equal(t, spec.RecursiveInfinite, merged.Recursive)
		assert.Equal(t, spec.CopyModeAllResources, merged.CopyMode)
		assert.Equal(t, spec.UploadAsLocalBlob, merged.UploadType)
	})

	t.Run("nil element is skipped", func(t *testing.T) {
		a := &spec.Config{CopyMode: spec.CopyModeAllResources}

		merged := spec.Merge(nil, a, nil)

		assert.Equal(t, spec.CopyModeAllResources, merged.CopyMode)
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
    copyMode: allResources
`)
		cfg, err := spec.LookupConfig(generic)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, spec.RecursiveInfinite, cfg.Recursive)
		assert.Equal(t, spec.CopyModeAllResources, cfg.CopyMode)
		assert.Empty(t, cfg.UploadType)
	})

	t.Run("later entry wins, unset fields fall through", func(t *testing.T) {
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    recursive: -1
    copyMode: localBlob
    uploadType: localBlob
  - type: transfer.config.ocm.software/v1alpha1
    copyMode: allResources
`)
		cfg, err := spec.LookupConfig(generic)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, spec.RecursiveInfinite, cfg.Recursive)
		assert.Equal(t, spec.CopyModeAllResources, cfg.CopyMode)
		assert.Equal(t, spec.UploadAsLocalBlob, cfg.UploadType)
	})

	t.Run("invalid entry is rejected", func(t *testing.T) {
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    copyMode: garbage
`)
		_, err := spec.LookupConfig(generic)
		require.ErrorContains(t, err, "invalid copyMode")
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
}
