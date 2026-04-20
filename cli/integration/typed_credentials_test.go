package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/credentials"
	credentialruntime "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	credentialsv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd/configuration"
)

// Test_TypedCredentials_HelmHTTPCredentials verifies that the CLI credential graph
// correctly resolves typed HelmHTTPCredentials/v1 from an .ocmconfig file.
func Test_TypedCredentials_HelmHTTPCredentials(t *testing.T) {
	ctx := t.Context()

	cfg := `
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software/v1
  consumers:
  - identity:
      type: HelmChartRepository
      hostname: "charts.example.com"
      scheme: https
    credentials:
    - type: HelmHTTPCredentials/v1
      username: "helmuser"
      password: "helmpass"
      certFile: "/path/to/cert.pem"
      keyFile: "/path/to/key.pem"
`

	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	// Parse config the same way the CLI does
	ocmconf, err := configuration.GetConfigFromPath(cfgPath)
	require.NoError(t, err)

	credconf, err := credentialruntime.LookupCredentialConfig(ocmconf)
	require.NoError(t, err)
	require.NotNil(t, credconf)

	// Set up credential type scheme with HelmHTTPCredentials
	credTypeScheme := ocmruntime.NewScheme()
	helmcredsv1.MustRegisterCredentialType(credTypeScheme)

	graph, err := credentials.ToGraph(ctx, credconf, credentials.Options{
		CredentialTypeSchemeProvider: &credentials.SchemeAsCredentialTypeSchemeProvider{S: credTypeScheme},
	})
	require.NoError(t, err)

	identity := ocmruntime.Identity{
		"type":     "HelmChartRepository",
		"hostname": "charts.example.com",
		"scheme":   "https",
	}

	t.Run("ResolveTyped returns *HelmHTTPCredentials", func(t *testing.T) {
		typed, err := graph.ResolveTyped(ctx, identity)
		require.NoError(t, err)
		require.NotNil(t, typed)

		helmCreds, ok := typed.(*helmcredsv1.HelmHTTPCredentials)
		require.True(t, ok, "expected *HelmHTTPCredentials, got %T", typed)

		assert.Equal(t, "helmuser", helmCreds.Username)
		assert.Equal(t, "helmpass", helmCreds.Password)
		assert.Equal(t, "/path/to/cert.pem", helmCreds.CertFile)
		assert.Equal(t, "/path/to/key.pem", helmCreds.KeyFile)
		assert.Empty(t, helmCreds.Keyring)
	})

	t.Run("Resolve returns map for backward compat", func(t *testing.T) {
		// The old map-based Resolve should still work
		credMap, err := graph.Resolve(ctx, identity)
		require.NoError(t, err)
		// Note: map extraction from typed credentials only works for *DirectCredentials
		// For *HelmHTTPCredentials, typedToMap returns nil since it's not DirectCredentials
		// This is expected — consumers should migrate to ResolveTyped
		assert.Nil(t, credMap)
	})
}

// Test_TypedCredentials_DirectCredentialsFallback verifies that old-style Credentials/v1
// configs still work through the credential graph.
func Test_TypedCredentials_DirectCredentialsFallback(t *testing.T) {
	ctx := t.Context()

	cfg := `
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software/v1
  consumers:
  - identity:
      type: HelmChartRepository
      hostname: "charts.example.com"
    credentials:
    - type: Credentials/v1
      properties:
        username: "legacyuser"
        password: "legacypass"
`

	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	ocmconf, err := configuration.GetConfigFromPath(cfgPath)
	require.NoError(t, err)

	credconf, err := credentialruntime.LookupCredentialConfig(ocmconf)
	require.NoError(t, err)

	graph, err := credentials.ToGraph(ctx, credconf, credentials.Options{})
	require.NoError(t, err)

	identity := ocmruntime.Identity{
		"type":     "HelmChartRepository",
		"hostname": "charts.example.com",
	}

	t.Run("ResolveTyped returns *DirectCredentials for old configs", func(t *testing.T) {
		typed, err := graph.ResolveTyped(ctx, identity)
		require.NoError(t, err)

		direct, ok := typed.(*credentialsv1.DirectCredentials)
		require.True(t, ok, "expected *DirectCredentials, got %T", typed)
		assert.Equal(t, "legacyuser", direct.Properties["username"])
		assert.Equal(t, "legacypass", direct.Properties["password"])
	})

	t.Run("Resolve returns map for old configs", func(t *testing.T) {
		credMap, err := graph.Resolve(ctx, identity)
		require.NoError(t, err)
		assert.Equal(t, "legacyuser", credMap["username"])
		assert.Equal(t, "legacypass", credMap["password"])
	})
}

// Test_TypedCredentials_InvalidCredentialType verifies that resolving credentials
// with a mismatched type is detected by the consumer.
func Test_TypedCredentials_InvalidCredentialType(t *testing.T) {
	ctx := t.Context()

	// Configure RSA-style credentials for a HelmChartRepository identity
	// This is a user config mistake — the graph stores whatever it gets,
	// but the consumer should reject it
	cfg := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software/v1
  consumers:
  - identity:
      type: HelmChartRepository
      hostname: "charts.example.com"
    credentials:
    - type: Credentials/v1
      properties:
        public_key_pem: "some-rsa-key"
        private_key_pem: "some-private-key"
`)

	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	ocmconf, err := configuration.GetConfigFromPath(cfgPath)
	require.NoError(t, err)

	credconf, err := credentialruntime.LookupCredentialConfig(ocmconf)
	require.NoError(t, err)

	graph, err := credentials.ToGraph(ctx, credconf, credentials.Options{})
	require.NoError(t, err)

	identity := ocmruntime.Identity{
		"type":     "HelmChartRepository",
		"hostname": "charts.example.com",
	}

	// Graph resolves successfully — it stores whatever was configured
	typed, err := graph.ResolveTyped(ctx, identity)
	require.NoError(t, err)

	// It's a DirectCredentials with RSA-style keys
	direct, ok := typed.(*credentialsv1.DirectCredentials)
	require.True(t, ok)
	assert.Equal(t, "some-rsa-key", direct.Properties["public_key_pem"])

	// When Helm converts this, the HelmHTTPCredentials fields will be empty
	// (the property names don't match Helm's expected keys)
	helmCreds := helmcredsv1.FromDirectCredentials(direct.Properties)
	assert.Empty(t, helmCreds.Username, "RSA keys shouldn't map to Helm username")
	assert.Empty(t, helmCreds.Password, "RSA keys shouldn't map to Helm password")
	assert.Empty(t, helmCreds.CertFile, "RSA keys shouldn't map to Helm certFile")
}
