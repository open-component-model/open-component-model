package spec_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

func TestLookupUploaderConfigs(t *testing.T) {
	decode := func(t *testing.T, yaml string) *genericv1.Config {
		t.Helper()
		var generic genericv1.Config
		require.NoError(t, genericv1.Scheme.Decode(strings.NewReader(yaml), &generic))
		return &generic
	}

	t.Run("example config with a transfer and an uploader entry", func(t *testing.T) {
		r := require.New(t)
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    copyMode: allResources
    uploadType: ociArtifact
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1alpha1
    targetURL: '${"https://mytarget.registry.com/uploads" + url(resource.access.url).path}'
    method: PUT
`)

		uploaders, err := spec.LookupUploaderConfigs(generic)
		r.NoError(err)
		r.Len(uploaders, 1)
		u, ok := uploaders[0].(*spec.HTTPUploaderConfig)
		r.True(ok)
		assert.Equal(t, "Wget", u.MatchSpec.AccessType.Name)
		assert.Equal(t, "v1alpha1", u.MatchSpec.AccessType.Version)
		assert.Equal(t, `${"https://mytarget.registry.com/uploads" + url(resource.access.url).path}`, u.TargetURL)
		assert.Equal(t, "PUT", u.Method)

		// The sibling transfer config is unaffected by the uploader entry.
		cfg, err := spec.LookupConfig(generic)
		r.NoError(err)
		r.NotNil(cfg)
		assert.Equal(t, spec.CopyModeAllResources, cfg.CopyMode)
		assert.Equal(t, spec.UploadAsOciArtifact, cfg.UploadType)
	})

	t.Run("no uploader entries returns nil", func(t *testing.T) {
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    copyMode: allResources
`)
		uploaders, err := spec.LookupUploaderConfigs(generic)
		require.NoError(t, err)
		assert.Nil(t, uploaders)
	})

	t.Run("multiple uploader entries preserve order", func(t *testing.T) {
		r := require.New(t)
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1alpha1
    targetURL: '${"https://first.example/uploads" + url(resource.access.url).path}'
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: S3/v2
    targetURL: '${"https://second.example/uploads" + url(resource.access.url).path}'
`)
		uploaders, err := spec.LookupUploaderConfigs(generic)
		r.NoError(err)
		r.Len(uploaders, 2)
		first, ok := uploaders[0].(*spec.HTTPUploaderConfig)
		r.True(ok)
		second, ok := uploaders[1].(*spec.HTTPUploaderConfig)
		r.True(ok)
		assert.Equal(t, "Wget", first.MatchSpec.AccessType.Name)
		assert.Equal(t, "S3", second.MatchSpec.AccessType.Name)
	})

	t.Run("missing match access type is rejected", func(t *testing.T) {
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    targetURL: '${"https://example/uploads" + url(resource.access.url).path}'
`)
		_, err := spec.LookupUploaderConfigs(generic)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "match.accessType is required")
	})

	t.Run("missing targetURL is rejected", func(t *testing.T) {
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1alpha1
`)
		_, err := spec.LookupUploaderConfigs(generic)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "targetURL is required")
	})
}

func TestHTTPUploaderConfig_Validate(t *testing.T) {
	valid := func() *spec.HTTPUploaderConfig {
		return &spec.HTTPUploaderConfig{
			MatchSpec: spec.UploaderMatch{AccessType: runtime.NewVersionedType("Wget", "v1alpha1")},
			TargetURL: `${"https://example/uploads"}`,
		}
	}

	t.Run("valid", func(t *testing.T) {
		require.NoError(t, valid().Validate())
	})

	t.Run("wrong type", func(t *testing.T) {
		u := valid()
		u.Type = runtime.NewVersionedType("other.config.ocm.software", "v1alpha1")
		require.Error(t, u.Validate())
	})

	t.Run("missing targetURL", func(t *testing.T) {
		u := valid()
		u.TargetURL = ""
		require.Error(t, u.Validate())
	})
}
