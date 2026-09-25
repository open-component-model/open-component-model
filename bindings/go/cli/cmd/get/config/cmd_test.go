package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	versioningv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/versioning/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const versioningEntry = `{
  "type": "versioning.config.ocm.software/v1alpha1",
  "schemes": [
    {
      "name": "calver-full",
      "pattern": "^(?P<year>\\d{4})\\.(?P<month>\\d{2})\\.(?P<day>\\d{2})$",
      "comparisonGroups": ["year", "month", "day"]
    }
  ]
}`

// TestGetEffectiveConfig_IncludesVersioning verifies that a configured versioning
// scheme is surfaced in the effective configuration, so "ocm get config" proves
// the active scheme rather than silently omitting it.
func TestGetEffectiveConfig_IncludesVersioning(t *testing.T) {
	r := require.New(t)

	raw := &runtime.Raw{}
	r.NoError(raw.UnmarshalJSON([]byte(versioningEntry)))
	cfg := &genericv1.Config{
		Type:           runtime.NewVersionedType(genericv1.ConfigType, genericv1.ConfigTypeV1),
		Configurations: []*runtime.Raw{raw},
	}

	eff, err := getEffectiveConfig(cfg)
	r.NoError(err)

	var found *versioningv1alpha1.Config
	for _, entry := range eff.Configurations {
		if vc, ok := entry.(*versioningv1alpha1.Config); ok {
			found = vc
			break
		}
	}
	r.NotNil(found, "versioning config must appear in effective configuration")
	r.Len(found.Schemes, 1)
	r.Equal("calver-full", found.Schemes[0].Name)
}

// TestGetEffectiveConfig_OmitsVersioningWhenAbsent ensures the versioning entry
// is not synthesized when the user configured no versioning schemes.
func TestGetEffectiveConfig_OmitsVersioningWhenAbsent(t *testing.T) {
	r := require.New(t)

	cfg := &genericv1.Config{
		Type:           runtime.NewVersionedType(genericv1.ConfigType, genericv1.ConfigTypeV1),
		Configurations: []*runtime.Raw{},
	}

	eff, err := getEffectiveConfig(cfg)
	r.NoError(err)
	for _, entry := range eff.Configurations {
		_, ok := entry.(*versioningv1alpha1.Config)
		r.False(ok, "versioning config must be absent without configured schemes")
	}
}
