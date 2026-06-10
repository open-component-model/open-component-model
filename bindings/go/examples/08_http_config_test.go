// Step 8: HTTP Client Configuration
//
// What you'll learn:
//   - Configuring global HTTP timeouts for all OCM operations
//   - Setting per-host timeout overrides for slow or distant registries
//   - Passing HTTP configuration through the OCM generic config system
//   - Wiring the resolved config into the OCI component version provider
//
// Constrained environments — corporate proxies, air-gapped networks, or
// registries with high latency — often need tighter control over HTTP client
// behaviour than the defaults provide. OCM exposes this through
// http.config.ocm.software/v1alpha1, which you embed in a generic OCM config
// and hand to the OCI and Helm providers.
//
// The pattern is always the same:
//  1. Build a genericv1.Config with an http.config.ocm.software/v1alpha1 entry.
//  2. Call httpv1alpha1.ResolveHTTPConfig to validate and extract the settings.
//  3. Pass the resolved *httpv1alpha1.Config to providers via WithHTTPConfig.

package examples

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
)

// TestExample_HTTPConfig_Defaults shows the zero-config case: when no HTTP
// configuration is present in the OCM config, ResolveHTTPConfig returns a
// Config with the built-in 30-second default timeout.
func TestExample_HTTPConfig_Defaults(t *testing.T) {
	r := require.New(t)

	// A nil generic config is valid. ResolveHTTPConfig still returns a
	// non-nil Config carrying DefaultTimeout (30s).
	httpCfg, err := httpv1alpha1.ResolveHTTPConfig(nil)
	r.NoError(err)
	r.NotNil(httpCfg)
	r.NotNil(httpCfg.Timeout)
	r.Equal(httpv1alpha1.Timeout(30*time.Second), *httpCfg.Timeout,
		"default timeout should be 30s")
}

// TestExample_HTTPConfig_GlobalTimeout shows how to set a single global
// timeout that applies to every outbound HTTP request OCM makes.
//
// Use this when you need a stricter (or more relaxed) deadline than 30s
// across all registries.
func TestExample_HTTPConfig_GlobalTimeout(t *testing.T) {
	r := require.New(t)

	// 1. Embed an http.config.ocm.software/v1alpha1 block in the generic OCM
	//    config.  The YAML key names are the same whether you write the config
	//    to ~/.ocmconfig or build it programmatically.
	const yamlConfig = `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 10s
    tlsHandshakeTimeout: 5s
    responseHeaderTimeout: 8s
`

	var cfg genericv1.Config
	err := genericv1.Scheme.Decode(strings.NewReader(yamlConfig), &cfg)
	r.NoError(err)

	// 2. Resolve and validate. Any negative timeout is rejected here rather
	//    than silently ignored at request time.
	httpCfg, err := httpv1alpha1.ResolveHTTPConfig(&cfg)
	r.NoError(err)

	r.Equal(httpv1alpha1.Timeout(10*time.Second), *httpCfg.Timeout)
	r.Equal(httpv1alpha1.Timeout(5*time.Second), *httpCfg.TLSHandshakeTimeout)
	r.Equal(httpv1alpha1.Timeout(8*time.Second), *httpCfg.ResponseHeaderTimeout)
}

// TestExample_HTTPConfig_PerHostOverrides shows how to apply different timeout
// budgets to individual registries while keeping a shorter global default.
//
// A common scenario: your internal Artifactory at artifactory.corp:5000 sits
// behind a slow WAN link and needs a 2-minute timeout, while all public
// registries should fail fast at 15s.
func TestExample_HTTPConfig_PerHostOverrides(t *testing.T) {
	r := require.New(t)

	const yamlConfig = `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 15s
    hosts:
      "artifactory.corp:5000":
        timeout: 2m
      "ghcr.io:443":
        timeout: 30s
        tlsHandshakeTimeout: 10s
`

	var cfg genericv1.Config
	err := genericv1.Scheme.Decode(strings.NewReader(yamlConfig), &cfg)
	r.NoError(err)

	httpCfg, err := httpv1alpha1.ResolveHTTPConfig(&cfg)
	r.NoError(err)

	// Global default applies to every host not listed under "hosts".
	r.Equal(httpv1alpha1.Timeout(15*time.Second), *httpCfg.Timeout)

	// Per-host overrides are stored in the Hosts map keyed by hostname:port.
	corpHost := httpCfg.Hosts["artifactory.corp:5000"]
	r.NotNil(corpHost)
	r.Equal(httpv1alpha1.Timeout(2*time.Minute), *corpHost.Timeout)

	ghcrHost := httpCfg.Hosts["ghcr.io:443"]
	r.NotNil(ghcrHost)
	r.Equal(httpv1alpha1.Timeout(30*time.Second), *ghcrHost.Timeout)
	r.Equal(httpv1alpha1.Timeout(10*time.Second), *ghcrHost.TLSHandshakeTimeout)
}

// TestExample_HTTPConfig_OCIProvider shows the end-to-end wiring: resolved
// HTTP config passed into the OCI component version provider so that every
// registry operation honours the configured timeouts.
//
// provider.WithHTTPConfig is the handoff point: the provider builds the
// *http.Client internally from the config, so callers never have to import
// bindings/go/http directly when working at the OCI layer.
func TestExample_HTTPConfig_OCIProvider(t *testing.T) {
	r := require.New(t)

	const yamlConfig = `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 45s
    tlsHandshakeTimeout: 10s
    hosts:
      "registry.example.com:443":
        timeout: 90s
`

	var cfg genericv1.Config
	err := genericv1.Scheme.Decode(strings.NewReader(yamlConfig), &cfg)
	r.NoError(err)

	httpCfg, err := httpv1alpha1.ResolveHTTPConfig(&cfg)
	r.NoError(err)

	// Pass the resolved config to the OCI component version provider.
	// Every push, pull, and list operation to registry.example.com will use
	// the 90s per-host timeout; all other registries get 45s.
	p := provider.NewComponentVersionRepositoryProvider(
		provider.WithHTTPConfig(httpCfg),
		provider.WithTempDir(t.TempDir()),
	)
	r.NotNil(p)
}

// TestExample_HTTPConfig_Merge shows that multiple
// http.config.ocm.software/v1alpha1 entries in the same generic config are
// merged: the last non-nil value for each field wins, and Hosts maps are
// combined entry-by-entry.
//
// This is useful when config comes from layered sources (organisation-wide
// defaults + team overrides + local developer settings).
func TestExample_HTTPConfig_Merge(t *testing.T) {
	r := require.New(t)

	// Two separate config blocks: org-wide defaults and a team-level override
	// that tightens the TLS handshake and adds a new host entry.
	const yamlConfig = `
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 60s
    tlsHandshakeTimeout: 30s
    hosts:
      "shared-registry.corp:443":
        timeout: 2m
  - type: http.config.ocm.software/v1alpha1
    tlsHandshakeTimeout: 5s
    hosts:
      "team-registry.corp:443":
        timeout: 45s
`

	var cfg genericv1.Config
	err := genericv1.Scheme.Decode(strings.NewReader(yamlConfig), &cfg)
	r.NoError(err)

	httpCfg, err := httpv1alpha1.ResolveHTTPConfig(&cfg)
	r.NoError(err)

	// timeout from the first block; tlsHandshakeTimeout overridden by second.
	r.Equal(httpv1alpha1.Timeout(60*time.Second), *httpCfg.Timeout)
	r.Equal(httpv1alpha1.Timeout(5*time.Second), *httpCfg.TLSHandshakeTimeout)

	// Hosts from both blocks are present.
	r.Contains(httpCfg.Hosts, "shared-registry.corp:443")
	r.Contains(httpCfg.Hosts, "team-registry.corp:443")
}
