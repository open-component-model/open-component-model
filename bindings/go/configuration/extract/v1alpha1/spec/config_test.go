package spec

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func mediaTypeRule(filename string, mediaTypes ...string) Rule {
	return Rule{
		Filename: filename,
		LayerSelectors: []*LayerSelector{
			{
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      LayerMediaTypeKey,
						Operator: LayerSelectorOpIn,
						Values:   mediaTypes,
					},
				},
			},
		},
	}
}

func TestMerge(t *testing.T) {
	t.Run("no configs returns nil", func(t *testing.T) {
		assert.Nil(t, Merge())
	})

	t.Run("merged config is defaulted to the versioned type", func(t *testing.T) {
		merged := Merge(&Config{})

		require.NotNil(t, merged)
		assert.Equal(t, runtime.NewVersionedType(ConfigType, Version), merged.Type)
		assert.Empty(t, merged.Rules)
	})

	t.Run("nil elements are skipped", func(t *testing.T) {
		merged := Merge(nil, &Config{Rules: []Rule{mediaTypeRule("chart.tgz", "application/tar+gzip")}}, nil)

		require.NotNil(t, merged)
		require.Len(t, merged.Rules, 1)
		assert.Equal(t, "chart.tgz", merged.Rules[0].Filename)
	})

	t.Run("last config declaring rules wins", func(t *testing.T) {
		a := &Config{Rules: []Rule{
			mediaTypeRule("a.tgz", "media/a"),
			mediaTypeRule("shared.tgz", "media/old"),
		}}
		b := &Config{Rules: []Rule{mediaTypeRule("shared.tgz", "media/new")}}

		merged := Merge(a, b)

		require.Len(t, merged.Rules, 1)
		assert.Equal(t, "shared.tgz", merged.Rules[0].Filename)
		require.Len(t, merged.Rules[0].LayerSelectors, 1)
		assert.Equal(t, []string{"media/new"}, merged.Rules[0].LayerSelectors[0].MatchExpressions[0].Values)
	})

	t.Run("config without rules keeps the previous rules", func(t *testing.T) {
		a := &Config{Rules: []Rule{mediaTypeRule("a.tgz", "media/a")}}

		merged := Merge(a, &Config{})

		require.Len(t, merged.Rules, 1)
		assert.Equal(t, "a.tgz", merged.Rules[0].Filename)
	})

	t.Run("merged config does not alias the input", func(t *testing.T) {
		original := &Config{Rules: []Rule{mediaTypeRule("chart.tgz", "media/a")}}

		merged := Merge(original)
		merged.Rules[0].Filename = "changed.tgz"
		merged.Rules[0].LayerSelectors[0].MatchExpressions[0].Values[0] = "media/changed"

		assert.Equal(t, "chart.tgz", original.Rules[0].Filename)
		assert.Equal(t, []string{"media/a"}, original.Rules[0].LayerSelectors[0].MatchExpressions[0].Values)
	})
}

func TestLookupConfig(t *testing.T) {
	decode := func(t *testing.T, data string) *v1.Config {
		t.Helper()
		var generic v1.Config
		require.NoError(t, v1.Scheme.Decode(strings.NewReader(data), &generic))
		return &generic
	}

	t.Run("nil config yields an empty config", func(t *testing.T) {
		cfg, err := LookupConfig(nil)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Empty(t, cfg.Rules)
	})

	t.Run("no extract entries yields an empty config", func(t *testing.T) {
		cfg, err := LookupConfig(decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: other.config.ocm.software/v1
`))
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Empty(t, cfg.Rules)
	})

	t.Run("last of versioned and unversioned entries wins", func(t *testing.T) {
		cfg, err := LookupConfig(decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: extract.oci.artifact.ocm.software/v1alpha1
    rules:
      - filename: chart.tgz
        layerSelectors:
          - matchExpressions:
              - key: layer.mediaType
                operator: In
                values:
                  - application/vnd.cncf.helm.chart.content.v1.tar+gzip
  - type: extract.oci.artifact.ocm.software
    rules:
      - filename: chart.tgz
        layerSelectors:
          - matchExpressions:
              - key: layer.mediaType
                operator: In
                values:
                  - application/tar+gzip
      - filename: chart.prov
        layerSelectors:
          - matchProperties:
              layer.mediaType: application/vnd.cncf.helm.chart.provenance.v1.prov
`))
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, runtime.NewVersionedType(ConfigType, Version), cfg.Type)

		require.Len(t, cfg.Rules, 2)
		assert.Equal(t, "chart.tgz", cfg.Rules[0].Filename)
		require.Len(t, cfg.Rules[0].LayerSelectors, 1)
		assert.Equal(t, []string{"application/tar+gzip"}, cfg.Rules[0].LayerSelectors[0].MatchExpressions[0].Values)
		assert.Equal(t, "chart.prov", cfg.Rules[1].Filename)
	})
}
