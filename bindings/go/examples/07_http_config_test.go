// Step 7: HTTP Client Configuration
//
// What you'll learn:
//   - Wiring resolved HTTP config into the OCI component version provider
//
// The pattern:
//  1. Build a genericv1.Config with an http.config.ocm.software/v1alpha1 entry.
//  2. Call httpv1alpha1.ResolveHTTPConfig to validate and extract the settings.
//  3. Pass the resolved *httpv1alpha1.Config to providers via WithHTTPConfig.

package examples

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
)

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
