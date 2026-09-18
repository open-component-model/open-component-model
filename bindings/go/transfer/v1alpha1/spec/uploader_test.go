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
  - type: uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1alpha1
    stream:
      type: HTTPStreaming/v1alpha1
      targetURL: 'https://mytarget.registry.com/{{.path}}'
      method: PUT
      queryParams: {}
`)

		uploaders, err := spec.LookupUploaderConfigs(generic)
		r.NoError(err)
		r.Len(uploaders, 1)
		assert.Equal(t, "Wget", uploaders[0].Match.AccessType.Name)
		assert.Equal(t, "v1alpha1", uploaders[0].Match.AccessType.Version)
		r.NotNil(uploaders[0].Stream)
		assert.Equal(t, "HTTPStreaming", uploaders[0].Stream.GetType().Name)
		assert.Equal(t, "v1alpha1", uploaders[0].Stream.GetType().Version)

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
  - type: uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1alpha1
    stream:
      type: HTTPStreaming/v1alpha1
      targetURL: 'https://first.example/{{.path}}'
  - type: uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: S3/v2
    stream:
      type: HTTPStreaming/v1alpha1
      targetURL: 'https://second.example/{{.path}}'
`)
		uploaders, err := spec.LookupUploaderConfigs(generic)
		r.NoError(err)
		r.Len(uploaders, 2)
		assert.Equal(t, "Wget", uploaders[0].Match.AccessType.Name)
		assert.Equal(t, "S3", uploaders[1].Match.AccessType.Name)
	})

	t.Run("missing match access type is rejected", func(t *testing.T) {
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: uploader.transfer.config.ocm.software/v1alpha1
    stream:
      type: HTTPStreaming/v1alpha1
      targetURL: 'https://example/{{.path}}'
`)
		_, err := spec.LookupUploaderConfigs(generic)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "match.accessType is required")
	})

	t.Run("missing stream is rejected", func(t *testing.T) {
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1alpha1
`)
		_, err := spec.LookupUploaderConfigs(generic)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stream is required")
	})
}

func TestUploaderConfig_Validate(t *testing.T) {
	valid := func() *spec.UploaderConfig {
		return &spec.UploaderConfig{
			Match: spec.UploaderMatch{AccessType: runtime.NewVersionedType("Wget", "v1alpha1")},
			Stream: &runtime.Raw{
				Type: runtime.NewVersionedType("HTTPStreaming", "v1alpha1"),
				Data: []byte(`{"type":"HTTPStreaming/v1alpha1"}`),
			},
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

	t.Run("untyped stream", func(t *testing.T) {
		u := valid()
		u.Stream = &runtime.Raw{Data: []byte(`{}`)}
		require.Error(t, u.Validate())
	})
}
