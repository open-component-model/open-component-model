package spec_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
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

// resourceWithIdentity builds a v2 Wget resource carrying extra identity attributes so
// the match's name/version/extraIdentity selection can be exercised.
func resourceWithIdentity(name, version string, extra map[string]string) descriptorv2.Resource {
	res := descriptorv2.Resource{
		ElementMeta: descriptorv2.ElementMeta{
			ObjectMeta: descriptorv2.ObjectMeta{Name: name, Version: version},
		},
		Type:     "blob",
		Relation: descriptorv2.ExternalRelation,
		Access: &runtime.Raw{
			Type: runtime.NewVersionedType("Wget", "v1"),
			Data: []byte(`{"type":"Wget/v1","url":"https://source.example/` + name + `"}`),
		},
	}
	if len(extra) > 0 {
		res.ExtraIdentity = runtime.Identity{}
		for k, v := range extra {
			res.ExtraIdentity[k] = v
		}
	}
	return res
}

// firstMatch returns the name of the first uploader whose Match applies, mirroring the
// first-recognized-match-wins selection the graph builder performs.
func firstMatch(uploaders []spec.UploaderConfig, resource descriptorv2.Resource) string {
	for _, u := range uploaders {
		if u != nil && u.Match(resource) {
			return strings.Trim(u.(*spec.HTTPUploaderConfig).TargetURL, "${\"}")
		}
	}
	return ""
}

func TestHTTPUploaderConfig_Match(t *testing.T) {
	wget := runtime.NewVersionedType("Wget", "v1")
	// Rules are named via their (single) targetURL literal so assertions can identify
	// which one won.
	rule := func(name string, m spec.UploaderMatch) *spec.HTTPUploaderConfig {
		return &spec.HTTPUploaderConfig{MatchSpec: m, TargetURL: `${"` + name + `"}`}
	}

	byAccess := rule("byAccess", spec.UploaderMatch{AccessType: wget})
	byName := rule("byName", spec.UploaderMatch{AccessType: wget, Name: "docs"})
	byVersion := rule("byVersion", spec.UploaderMatch{AccessType: wget, Version: "1.0.0"})
	byArch := rule("byArch", spec.UploaderMatch{AccessType: wget, ExtraIdentity: runtime.Identity{"architecture": "arm64"}})
	byNameAndArch := rule("byNameAndArch", spec.UploaderMatch{AccessType: wget, Name: "docs", ExtraIdentity: runtime.Identity{"architecture": "arm64"}})
	byVersionedAccess := rule("byVersionedAccess", spec.UploaderMatch{AccessType: runtime.NewVersionedType("Wget", "v2")})
	ociOnly := rule("ociOnly", spec.UploaderMatch{AccessType: runtime.NewVersionedType("OCIImage", "v1")})
	byUnversioned := rule("byUnversioned", spec.UploaderMatch{AccessType: runtime.NewUnversionedType("Wget")})

	tests := []struct {
		name      string
		uploaders []spec.UploaderConfig
		resource  descriptorv2.Resource
		want      string // winning rule name, "" for no match
	}{
		{"access type only", []spec.UploaderConfig{byAccess}, resourceWithIdentity("anything", "1.0.0", nil), "byAccess"},
		{"name constraint selects only the named resource", []spec.UploaderConfig{byName}, resourceWithIdentity("other", "1.0.0", nil), ""},
		{"name constraint matches the named resource", []spec.UploaderConfig{byName}, resourceWithIdentity("docs", "1.0.0", nil), "byName"},
		{"version constraint matches the versioned resource", []spec.UploaderConfig{byVersion}, resourceWithIdentity("docs", "1.0.0", nil), "byVersion"},
		{"version constraint selects only the matching version", []spec.UploaderConfig{byVersion}, resourceWithIdentity("docs", "2.0.0", nil), ""},
		{"extraIdentity must be present and equal", []spec.UploaderConfig{byArch}, resourceWithIdentity("docs", "1.0.0", map[string]string{"architecture": "amd64"}), ""},
		{"extraIdentity matches", []spec.UploaderConfig{byArch}, resourceWithIdentity("docs", "1.0.0", map[string]string{"architecture": "arm64"}), "byArch"},
		{"first match wins: specific before broad", []spec.UploaderConfig{byNameAndArch, byName, byAccess}, resourceWithIdentity("docs", "1.0.0", map[string]string{"architecture": "arm64"}), "byNameAndArch"},
		{"broad rule wins when specific rules do not apply", []spec.UploaderConfig{byNameAndArch, byName, byAccess}, resourceWithIdentity("other", "1.0.0", nil), "byAccess"},
		{"access version must match when specified", []spec.UploaderConfig{byVersionedAccess}, resourceWithIdentity("docs", "1.0.0", nil), ""},
		{"unversioned access type matches any version", []spec.UploaderConfig{byUnversioned}, resourceWithIdentity("docs", "1.0.0", nil), "byUnversioned"},
		{"non-matching access type is skipped", []spec.UploaderConfig{ociOnly, byAccess}, resourceWithIdentity("docs", "1.0.0", nil), "byAccess"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, firstMatch(tc.uploaders, tc.resource))
		})
	}
}
