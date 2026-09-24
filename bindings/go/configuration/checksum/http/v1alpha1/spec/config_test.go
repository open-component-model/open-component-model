package spec_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	v1alpha1 "ocm.software/open-component-model/bindings/go/configuration/checksum/http/v1alpha1/spec"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
)

// helper: unpack a versioned Config from a YAML-encoded generic config so tests
// exercise the same code path a real OCM CLI load takes.
func decodeGeneric(t *testing.T, yaml string) *genericv1.Config {
	t.Helper()
	var cfg genericv1.Config
	require.NoError(t, genericv1.Scheme.Decode(strings.NewReader(yaml), &cfg))
	return &cfg
}

func TestLookupConfig_Mode(t *testing.T) {
	r := require.New(t)
	got, err := v1alpha1.LookupConfig(decodeGeneric(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: checksum.http.config.ocm.software/v1alpha1
    mode: Skip
`))
	r.NoError(err)
	r.NotNil(got)
	r.Equal(v1alpha1.ChecksumModeSkip, got.Mode)
}

func TestLookupConfig_ReturnsNilWhenAbsent(t *testing.T) {
	r := require.New(t)
	got, err := v1alpha1.LookupConfig(decodeGeneric(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: some.other.config.ocm.software/v1alpha1
`))
	r.NoError(err)
	r.Nil(got)
}

func TestModeForURL_UnsetDefaultsToPrefer(t *testing.T) {
	r := require.New(t)
	got, err := v1alpha1.LookupConfig(decodeGeneric(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: checksum.http.config.ocm.software/v1alpha1
    hosts:
      "repo.example.com":
        mode: Require
`))
	r.NoError(err)
	// Top-level mode unset: a non-matching host resolves to the default.
	r.Equal(v1alpha1.ChecksumModePrefer, got.ModeForURL("https://other.example.com/artifact"))
}

func TestModeForURL_HostOverrideWinsOverDefault(t *testing.T) {
	r := require.New(t)
	cfg, err := v1alpha1.LookupConfig(decodeGeneric(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: checksum.http.config.ocm.software/v1alpha1
    mode: Skip
    hosts:
      "repo.example.com":
        mode: Require
`))
	r.NoError(err)

	// Host match: the override wins.
	r.Equal(v1alpha1.ChecksumModeRequire, cfg.ModeForURL("https://repo.example.com/artifact.tar.gz"))
	// No host match: falls through to the default.
	r.Equal(v1alpha1.ChecksumModeSkip, cfg.ModeForURL("https://other.example.com/artifact.tar.gz"))
}

func TestModeForURL_PortQualifiedKeyBeatsBareHost(t *testing.T) {
	r := require.New(t)
	cfg, err := v1alpha1.LookupConfig(decodeGeneric(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: checksum.http.config.ocm.software/v1alpha1
    hosts:
      "repo.example.com":
        mode: Skip
      "repo.example.com:8443":
        mode: Require
`))
	r.NoError(err)
	r.Equal(v1alpha1.ChecksumModeRequire, cfg.ModeForURL("https://repo.example.com:8443/x"))
	// Bare-hostname entry applies to other ports (and to the implicit :443).
	r.Equal(v1alpha1.ChecksumModeSkip, cfg.ModeForURL("https://repo.example.com/x"))
}

func TestModeForURL_NilConfigYieldsDefault(t *testing.T) {
	r := require.New(t)
	var cfg *v1alpha1.Config
	r.Equal(v1alpha1.ChecksumModePrefer, cfg.ModeForURL("https://repo.example.com/x"))
}

func TestModeForURL_MalformedURLFallsBackToDefault(t *testing.T) {
	r := require.New(t)
	cfg, err := v1alpha1.LookupConfig(decodeGeneric(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: checksum.http.config.ocm.software/v1alpha1
    mode: Skip
    hosts:
      "repo.example.com":
        mode: Require
`))
	r.NoError(err)
	// A URL that parses with no host must not match a host override.
	r.Equal(v1alpha1.ChecksumModeSkip, cfg.ModeForURL("://missing-scheme"),
		"malformed URL falls back to the default mode, not to a host override")
}

func TestMerge_LaterWins(t *testing.T) {
	r := require.New(t)
	merged, err := v1alpha1.LookupConfig(decodeGeneric(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: checksum.http.config.ocm.software/v1alpha1
    mode: Skip
    hosts:
      "a.example":
        mode: Prefer
  - type: checksum.http.config.ocm.software/v1alpha1
    mode: Require
    hosts:
      "b.example":
        mode: Skip
`))
	r.NoError(err)
	// Later default wins.
	r.Equal(v1alpha1.ChecksumModeRequire, merged.Mode)
	// Hosts maps are unioned.
	r.Contains(merged.Hosts, "a.example")
	r.Contains(merged.Hosts, "b.example")
}

func TestModeForURL_MixedCaseHostnameMatchesLowercasedConfigKey(t *testing.T) {
	r := require.New(t)
	cfg, err := v1alpha1.LookupConfig(decodeGeneric(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: checksum.http.config.ocm.software/v1alpha1
    hosts:
      "repo.example.com":
        mode: Require
`))
	r.NoError(err)
	r.Equal(v1alpha1.ChecksumModeRequire, cfg.ModeForURL("https://Repo.Example.COM/artifact"),
		"mixed-case URL hostname must match a lowercased config key")

	// Mixed case in the config too — should still match a lowercased URL.
	cfg2, err := v1alpha1.LookupConfig(decodeGeneric(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: checksum.http.config.ocm.software/v1alpha1
    hosts:
      "Repo.Example.COM":
        mode: Require
`))
	r.NoError(err)
	r.Equal(v1alpha1.ChecksumModeRequire, cfg2.ModeForURL("https://repo.example.com/artifact"),
		"mixed-case config key must match a lowercased URL hostname")
}

func TestModeForURL_TerminalDotHostnameMatchesConfigKey(t *testing.T) {
	r := require.New(t)
	cfg, err := v1alpha1.LookupConfig(decodeGeneric(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: checksum.http.config.ocm.software/v1alpha1
    hosts:
      "repo.example.com":
        mode: Require
`))
	r.NoError(err)
	r.Equal(v1alpha1.ChecksumModeRequire, cfg.ModeForURL("https://repo.example.com./artifact"),
		"trailing-dot URL host must match an undotted config key")

	// Symmetric: config keyed with trailing dot must match undotted URL.
	cfg2, err := v1alpha1.LookupConfig(decodeGeneric(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: checksum.http.config.ocm.software/v1alpha1
    hosts:
      "repo.example.com.":
        mode: Require
`))
	r.NoError(err)
	r.Equal(v1alpha1.ChecksumModeRequire, cfg2.ModeForURL("https://repo.example.com/artifact"),
		"undotted URL host must match a trailing-dot config key")
}

func TestLookupConfig_RejectsInvalidTopLevelMode(t *testing.T) {
	r := require.New(t)
	_, err := v1alpha1.LookupConfig(decodeGeneric(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: checksum.http.config.ocm.software/v1alpha1
    mode: Requre
`))
	r.Error(err, "a typo such as Requre must not silently disable verification")
	r.Contains(err.Error(), "Requre")
}

func TestLookupConfig_RejectsInvalidHostMode(t *testing.T) {
	r := require.New(t)
	_, err := v1alpha1.LookupConfig(decodeGeneric(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: checksum.http.config.ocm.software/v1alpha1
    mode: Require
    hosts:
      "repo.example.com":
        mode: Prefr
`))
	r.Error(err, "an unknown host-override mode must be rejected")
	r.Contains(err.Error(), "Prefr")
	r.Contains(err.Error(), "repo.example.com")
}

func TestLookupConfig_AllowsEmptyModesForDefaulting(t *testing.T) {
	r := require.New(t)
	cfg, err := v1alpha1.LookupConfig(decodeGeneric(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: checksum.http.config.ocm.software/v1alpha1
    hosts:
      "repo.example.com": {}
`))
	r.NoError(err, "empty modes are valid and resolve to the default")
	r.Equal(v1alpha1.ChecksumModePrefer, cfg.ModeForURL("https://repo.example.com/artifact"))
}
