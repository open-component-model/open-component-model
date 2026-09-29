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
    recursive: -1
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("Wget/v1alpha1")
    targetURL: '${"https://mytarget.registry.com/uploads" + url(resource.access.url).path}'
    method: PUT
`)

		uploaders, err := spec.LookupUploaderConfigs(generic)
		r.NoError(err)
		r.Len(uploaders, 1)
		u, ok := uploaders[0].(*spec.HTTPUploaderConfig)
		r.True(ok)
		assert.Equal(t, `resource.access.isType("Wget/v1alpha1")`, u.Match)
		assert.Equal(t, `${"https://mytarget.registry.com/uploads" + url(resource.access.url).path}`, u.TargetURL)
		assert.Equal(t, "PUT", u.Method)

		// The sibling transfer config is unaffected by the uploader entry.
		cfg, err := spec.LookupConfig(generic)
		r.NoError(err)
		r.NotNil(cfg)
		assert.Equal(t, spec.RecursiveInfinite, cfg.Recursive)
	})

	t.Run("no uploader entries returns nil", func(t *testing.T) {
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    recursive: -1
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
    match: resource.access.isType("Wget/v1alpha1")
    targetURL: '${"https://first.example/uploads" + url(resource.access.url).path}'
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("S3/v2")
    targetURL: '${"https://second.example/uploads" + url(resource.access.url).path}'
`)
		uploaders, err := spec.LookupUploaderConfigs(generic)
		r.NoError(err)
		r.Len(uploaders, 2)
		first, ok := uploaders[0].(*spec.HTTPUploaderConfig)
		r.True(ok)
		second, ok := uploaders[1].(*spec.HTTPUploaderConfig)
		r.True(ok)
		assert.Equal(t, `resource.access.isType("Wget/v1alpha1")`, first.Match)
		assert.Equal(t, `resource.access.isType("S3/v2")`, second.Match)
	})

	t.Run("missing match is rejected", func(t *testing.T) {
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    targetURL: '${"https://example/uploads" + url(resource.access.url).path}'
`)
		_, err := spec.LookupUploaderConfigs(generic)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "match is required")
	})

	t.Run("stale static match is rejected", func(t *testing.T) {
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match:
      name: app
`)
		_, err := spec.LookupUploaderConfigs(generic)
		require.ErrorContains(t, err, "failed to decode uploader config")
		require.ErrorContains(t, err, "match")
	})

	t.Run("missing targetURL is rejected", func(t *testing.T) {
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("Wget/v1alpha1")
`)
		_, err := spec.LookupUploaderConfigs(generic)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "targetURL is required")
	})

	t.Run("oci uploader without fields uses the default match, order is kept", func(t *testing.T) {
		r := require.New(t)
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("Wget/v1")
    targetURL: '${"https://first.example/uploads" + url(resource.access.url).path}'
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
  - type: oci.uploader.transfer.config.ocm.software
    match: resource.name == "docs"
    imageReference: ghcr.io/mirror/docs:1.0.0
`)
		uploaders, err := spec.LookupUploaderConfigs(generic)
		r.NoError(err)
		r.Len(uploaders, 3)
		r.IsType(&spec.HTTPUploaderConfig{}, uploaders[0])
		plain, ok := uploaders[1].(*spec.OCIUploaderConfig)
		r.True(ok)
		r.Empty(plain.Match)
		r.Empty(plain.ImageReference)
		r.Equal(spec.DefaultOCIUploaderMatch, plain.EffectiveMatch())

		byName, ok := uploaders[2].(*spec.OCIUploaderConfig)
		r.True(ok)
		r.Equal("ghcr.io/mirror/docs:1.0.0", byName.ImageReference)
		r.Equal(`resource.name == "docs"`, byName.EffectiveMatch())
	})

	t.Run("match is decoded", func(t *testing.T) {
		r := require.New(t)
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.name == "x" && resource.access.isType("OCIImage")
`)
		uploaders, err := spec.LookupUploaderConfigs(generic)
		r.NoError(err)
		r.Len(uploaders, 1)
		r.Equal(`resource.name == "x" && resource.access.isType("OCIImage")`, uploaders[0].EffectiveMatch())
	})

	t.Run("unknown uploader field is rejected", func(t *testing.T) {
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("Wget/v1")
    targetURL: '${"https://example/uploads"}'
    headers:
      X-Foo: [bar]
`)
		_, err := spec.LookupUploaderConfigs(generic)
		require.ErrorContains(t, err, `unknown field "headers"`)
	})
	t.Run("localblob and reference uploaders are decoded", func(t *testing.T) {
		r := require.New(t)
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
  - type: reference.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.name == "keep-this"
`)
		uploaders, err := spec.LookupUploaderConfigs(generic)
		r.NoError(err)
		r.Len(uploaders, 3)
		r.IsType(&spec.OCIUploaderConfig{}, uploaders[0])

		lb, ok := uploaders[1].(*spec.LocalBlobUploaderConfig)
		r.True(ok)
		r.Empty(lb.Match)

		ref, ok := uploaders[2].(*spec.ReferenceUploaderConfig)
		r.True(ok)
		r.Equal(`resource.name == "keep-this"`, ref.Match)
	})
}

func TestUploaderConfig_EffectiveMatch(t *testing.T) {
	const match = `resource.access.isType("Helm")`
	for _, tc := range []struct {
		name     string
		uploader spec.UploaderConfig
		want     string
	}{
		{"oci without match uses the default", &spec.OCIUploaderConfig{}, spec.DefaultOCIUploaderMatch},
		{"oci with blank match uses the default", &spec.OCIUploaderConfig{Match: "  "}, spec.DefaultOCIUploaderMatch},
		{"oci with match replaces the default", &spec.OCIUploaderConfig{Match: match}, match},
		{"http with match", &spec.HTTPUploaderConfig{Match: match}, match},
		{"http without match has none", &spec.HTTPUploaderConfig{}, ""},
		{"localblob without match uses the default", &spec.LocalBlobUploaderConfig{}, spec.DefaultLocalBlobUploaderMatch},
		{"localblob with match replaces the default", &spec.LocalBlobUploaderConfig{Match: match}, match},
		{"reference without match uses the default", &spec.ReferenceUploaderConfig{}, spec.DefaultReferenceUploaderMatch},
		{"reference with match replaces the default", &spec.ReferenceUploaderConfig{Match: match}, match},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.New(t).Equal(tc.want, tc.uploader.EffectiveMatch())
		})
	}
}

func TestHTTPUploaderConfig_Validate(t *testing.T) {
	valid := func() *spec.HTTPUploaderConfig {
		return &spec.HTTPUploaderConfig{
			Match:     `resource.access.isType("Wget")`,
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

	t.Run("blank match", func(t *testing.T) {
		u := valid()
		u.Match = " "
		require.ErrorContains(t, u.Validate(), "match is required")
	})
}

func TestLocalBlobUploaderConfig_Validate(t *testing.T) {
	t.Run("empty is valid", func(t *testing.T) {
		require.NoError(t, (&spec.LocalBlobUploaderConfig{}).Validate())
	})

	t.Run("unversioned type is valid", func(t *testing.T) {
		require.NoError(t, (&spec.LocalBlobUploaderConfig{
			Type: runtime.NewUnversionedType(spec.LocalBlobUploaderConfigType),
		}).Validate())
	})

	t.Run("wrong type is rejected", func(t *testing.T) {
		err := (&spec.LocalBlobUploaderConfig{
			Type: runtime.NewVersionedType("other.config.ocm.software", "v1alpha1"),
		}).Validate()
		require.ErrorContains(t, err, "invalid type")
	})
}

func TestReferenceUploaderConfig_Validate(t *testing.T) {
	t.Run("empty is valid", func(t *testing.T) {
		require.NoError(t, (&spec.ReferenceUploaderConfig{}).Validate())
	})

	t.Run("unversioned type is valid", func(t *testing.T) {
		require.NoError(t, (&spec.ReferenceUploaderConfig{
			Type: runtime.NewUnversionedType(spec.ReferenceUploaderConfigType),
		}).Validate())
	})

	t.Run("wrong type is rejected", func(t *testing.T) {
		err := (&spec.ReferenceUploaderConfig{
			Type: runtime.NewVersionedType("other.config.ocm.software", "v1alpha1"),
		}).Validate()
		require.ErrorContains(t, err, "invalid type")
	})
}
