package spec_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	versioningspec "ocm.software/open-component-model/bindings/go/configuration/versioning/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// makeGenericConfig creates a genericv1.Config from a list of raw JSON configuration entries.
func makeGenericConfig(t *testing.T, entries ...string) *genericv1.Config {
	t.Helper()
	cfg := &genericv1.Config{
		Type:           runtime.NewVersionedType(genericv1.ConfigType, genericv1.ConfigTypeV1),
		Configurations: make([]*runtime.Raw, 0, len(entries)),
	}
	for _, entry := range entries {
		raw := &runtime.Raw{}
		require.NoError(t, raw.UnmarshalJSON([]byte(entry)))
		cfg.Configurations = append(cfg.Configurations, raw)
	}
	return cfg
}

const calverEntry = `{
  "type": "versioning.config.ocm.software/v1alpha1",
  "schemes": [
    {
      "name": "calver-full",
      "pattern": "^(?P<year>\\d{4})\\.(?P<month>\\d{2})\\.(?P<day>\\d{2})$",
      "comparisonGroups": ["year", "month", "day"]
    }
  ]
}`

func TestLookup_RoundTripAndRegistryOrdersCalver(t *testing.T) {
	r := require.New(t)

	cfg, err := versioningspec.Lookup(makeGenericConfig(t, calverEntry))
	r.NoError(err)
	r.NotNil(cfg)
	r.Len(cfg.Schemes, 1)
	r.Equal("calver-full", cfg.Schemes[0].Name)

	reg, err := cfg.Registry()
	r.NoError(err)

	versions := []string{"2024.03.15", "2024.10.01", "2023.12.31"}
	r.NoError(reg.SortDescending(versions))
	r.Equal([]string{"2024.10.01", "2024.03.15", "2023.12.31"}, versions)

	// With only calver configured the built-in semver scheme is NOT appended, so
	// semver versions are not recognized as valid.
	r.False(reg.Valid("1.0.0"))
	r.Len(reg.Schemes(), 1)
}

const calverWithSemverEntry = `{
  "type": "versioning.config.ocm.software/v1alpha1",
  "schemes": [
    {
      "name": "calver-full",
      "pattern": "^(?P<year>\\d{4})\\.(?P<month>\\d{2})\\.(?P<day>\\d{2})$",
      "comparisonGroups": ["year", "month", "day"]
    },
    {
      "name": "semver",
      "builtin": "loose-semver"
    }
  ]
}`

func TestRegistry_BuiltinSemverFallbackOptIn(t *testing.T) {
	r := require.New(t)

	cfg, err := versioningspec.Lookup(makeGenericConfig(t, calverWithSemverEntry))
	r.NoError(err)
	reg, err := cfg.Registry()
	r.NoError(err)
	r.Len(reg.Schemes(), 2)

	// Both schemes now apply: calver and semver versions are valid, and semver
	// still orders numerically (via the built-in scheme, not lexically).
	r.True(reg.Valid("2024.03.15"))
	r.True(reg.Valid("1.0.0"))
	semvers := []string{"1.0.0", "2.0.0", "1.10.0", "1.5.0"}
	r.NoError(reg.SortDescending(semvers))
	r.Equal([]string{"2.0.0", "1.10.0", "1.5.0", "1.0.0"}, semvers)
}

func TestRegistry_BuiltinConflictsAndUnknown(t *testing.T) {
	r := require.New(t)

	// builtin is mutually exclusive with pattern.
	_, err := (&versioningspec.Config{
		Schemes: []*versioningspec.VersionScheme{
			{Name: "bad", Builtin: versioningspec.BuiltinLooseSemver, Pattern: "^v?.+$"},
		},
	}).Registry()
	r.Error(err)
	r.Contains(err.Error(), "mutually exclusive")

	// unknown builtin is rejected.
	_, err = (&versioningspec.Config{
		Schemes: []*versioningspec.VersionScheme{
			{Name: "bad", Builtin: "calver"},
		},
	}).Registry()
	r.Error(err)
	r.Contains(err.Error(), "unknown builtin")
}

func TestRegistry_NilOrEmptyConfigIsDefault(t *testing.T) {
	r := require.New(t)

	var empty *versioningspec.Config
	reg, err := empty.Registry()
	r.NoError(err)
	r.Len(reg.Schemes(), 1) // loose-semver only

	reg, err = (&versioningspec.Config{}).Registry()
	r.NoError(err)
	r.Len(reg.Schemes(), 1)
}

func TestRegistry_InvalidPatternFails(t *testing.T) {
	r := require.New(t)
	cfg := &versioningspec.Config{
		Schemes: []*versioningspec.VersionScheme{
			{Name: "broken", Pattern: "^(unclosed"},
		},
	}
	_, err := cfg.Registry()
	r.Error(err)
	r.Contains(err.Error(), "invalid pattern")
}

func TestRegistry_UnknownComparisonGroupFails(t *testing.T) {
	r := require.New(t)
	cfg := &versioningspec.Config{
		Schemes: []*versioningspec.VersionScheme{
			{Name: "calver", Pattern: `^(?P<year>\d{4})$`, ComparisonGroups: []string{"month"}},
		},
	}
	_, err := cfg.Registry()
	r.Error(err)
	r.Contains(err.Error(), "is not a named capture group")
}
