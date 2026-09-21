package v1alpha1_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	v1alpha1 "ocm.software/open-component-model/bindings/go/wget/spec/config/v1alpha1"
	inputv1 "ocm.software/open-component-model/bindings/go/wget/spec/input/v1"
)

// helper: unpack a versioned Config from a YAML-encoded generic config so tests
// exercise the same code path a real OCM CLI load takes.
func decodeGeneric(t *testing.T, yaml string) *genericv1.Config {
	t.Helper()
	var cfg genericv1.Config
	require.NoError(t, genericv1.Scheme.Decode(strings.NewReader(yaml), &cfg))
	return &cfg
}

func TestLookupConfig_Default(t *testing.T) {
	r := require.New(t)
	got, err := v1alpha1.LookupConfig(decodeGeneric(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: wget.config.ocm.software/v1alpha1
    defaultChecksumPolicy:
      onMissing: compute
      sources:
        - type: httpHeader
`))
	r.NoError(err)
	r.NotNil(got)
	r.NotNil(got.DefaultChecksumPolicy)
	r.Equal(inputv1.OnMissingCompute, got.DefaultChecksumPolicy.OnMissing)
	r.Len(got.DefaultChecksumPolicy.Sources, 1)
	r.Equal(inputv1.ChecksumSourceHTTPHeader, got.DefaultChecksumPolicy.Sources[0].Type)
}

func TestLookupConfig_ReturnsNilWhenAbsent(t *testing.T) {
	r := require.New(t)
	got, err := v1alpha1.LookupConfig(decodeGeneric(t, `
type: generic.config.ocm.software/v1
configurations: []
`))
	r.NoError(err)
	r.Nil(got)
}

func TestPolicyForURL_HostOverrideWinsOverDefault(t *testing.T) {
	r := require.New(t)
	cfg, err := v1alpha1.LookupConfig(decodeGeneric(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: wget.config.ocm.software/v1alpha1
    defaultChecksumPolicy:
      onMissing: compute
      sources: [{type: httpHeader}]
    hosts:
      "repo.example.com":
        checksumPolicy:
          onMissing: fail
          sources: [{type: externalUrl, algorithms: [sha256]}]
`))
	r.NoError(err)

	// Host match: the "repo.example.com" override wins over the default.
	got := cfg.PolicyForURL("https://repo.example.com/artifact.tar.gz")
	r.NotNil(got)
	r.Equal(inputv1.OnMissingFail, got.OnMissing)
	r.Equal(inputv1.ChecksumSourceExternalURL, got.Sources[0].Type)

	// No host match: falls through to the default.
	got = cfg.PolicyForURL("https://other.example.com/artifact.tar.gz")
	r.NotNil(got)
	r.Equal(inputv1.OnMissingCompute, got.OnMissing)
	r.Equal(inputv1.ChecksumSourceHTTPHeader, got.Sources[0].Type)
}

func TestPolicyForURL_PortQualifiedKeyBeatsBareHost(t *testing.T) {
	r := require.New(t)
	cfg, err := v1alpha1.LookupConfig(decodeGeneric(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: wget.config.ocm.software/v1alpha1
    hosts:
      "repo.example.com":
        checksumPolicy:
          onMissing: compute
          sources: [{type: httpHeader}]
      "repo.example.com:8443":
        checksumPolicy:
          onMissing: fail
          sources: [{type: externalUrl}]
`))
	r.NoError(err)

	// host:port entry wins for the specific port.
	got := cfg.PolicyForURL("https://repo.example.com:8443/x")
	r.Equal(inputv1.OnMissingFail, got.OnMissing)
	// Bare-hostname entry applies to other ports (and to the implicit :443).
	got = cfg.PolicyForURL("https://repo.example.com/x")
	r.Equal(inputv1.OnMissingCompute, got.OnMissing)
}

func TestPolicyForURL_NilConfigYieldsNil(t *testing.T) {
	var cfg *v1alpha1.Config
	require.Nil(t, cfg.PolicyForURL("https://any/x"))
}

func TestPolicyForURL_MalformedURLFallsBackToDefault(t *testing.T) {
	r := require.New(t)
	cfg, err := v1alpha1.LookupConfig(decodeGeneric(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: wget.config.ocm.software/v1alpha1
    defaultChecksumPolicy:
      onMissing: compute
      sources: [{type: httpHeader}]
    hosts:
      "should.not.match":
        checksumPolicy: {onMissing: fail, sources: [{type: externalUrl}]}
`))
	r.NoError(err)

	// A URL Parse can accept most junk; use a control character to force an error.
	got := cfg.PolicyForURL("http://\x7f/artifact")
	r.NotNil(got)
	r.Equal(inputv1.OnMissingCompute, got.OnMissing, "malformed URL falls back to the default policy, not to a host override")
}

func TestMerge_LaterWins(t *testing.T) {
	r := require.New(t)
	merged, err := v1alpha1.LookupConfig(decodeGeneric(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: wget.config.ocm.software/v1alpha1
    defaultChecksumPolicy:
      onMissing: compute
      sources: [{type: httpHeader}]
    hosts:
      "a.example":
        checksumPolicy: {onMissing: compute, sources: [{type: httpHeader}]}
  - type: wget.config.ocm.software/v1alpha1
    defaultChecksumPolicy:
      onMissing: fail
      sources: [{type: externalUrl}]
    hosts:
      "b.example":
        checksumPolicy: {onMissing: fail, sources: [{type: externalUrl}]}
`))
	r.NoError(err)
	r.NotNil(merged)
	// Later default wins.
	r.Equal(inputv1.OnMissingFail, merged.DefaultChecksumPolicy.OnMissing)
	// Hosts maps are unioned.
	r.Contains(merged.Hosts, "a.example")
	r.Contains(merged.Hosts, "b.example")
}
