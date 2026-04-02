package transformation

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/credentials"
	credentialruntime "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	credentialsv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	helmidentityv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/identity/v1"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestTypedCredentialResolution_HelmHTTPCredentials(t *testing.T) {
	// Config with HelmHTTPCredentials/v1 instead of Credentials/v1
	yaml := `
type: credentials.config.ocm.software/v1
consumers:
  - identity:
      type: HelmChartRepository
      hostname: "charts.example.com"
    credentials:
      - type: HelmHTTPCredentials/v1
        username: "helmuser"
        password: "helmpass"
        certFile: "/path/to/cert.pem"
        keyFile: "/path/to/key.pem"
        keyring: "/path/to/keyring"
`

	// Set up scheme that knows about DirectCredentials and HelmHTTPCredentials
	cfgScheme := runtime.NewScheme()
	credentialsv1.MustRegister(cfgScheme)

	credTypeScheme := runtime.NewScheme()
	helmcredsv1.MustRegisterCredentialType(credTypeScheme)

	var configv1 credentialsv1.Config
	require.NoError(t, cfgScheme.Decode(strings.NewReader(yaml), &configv1))
	config := credentialruntime.ConvertFromV1(&configv1)

	graph, err := credentials.ToGraph(t.Context(), config, credentials.Options{
		CredentialTypeScheme: credTypeScheme,
	})
	require.NoError(t, err)

	// Resolve with ResolveTyped — should get *HelmHTTPCredentials back
	identity := runtime.Identity{
		"type":     "HelmChartRepository",
		"hostname": "charts.example.com",
	}

	typed, err := graph.ResolveTyped(t.Context(), identity)
	require.NoError(t, err)
	require.NotNil(t, typed)

	helmCreds, ok := typed.(*helmcredsv1.HelmHTTPCredentials)
	require.True(t, ok, "expected *HelmHTTPCredentials, got %T", typed)

	assert.Equal(t, "helmuser", helmCreds.Username)
	assert.Equal(t, "helmpass", helmCreds.Password)
	assert.Equal(t, "/path/to/cert.pem", helmCreds.CertFile)
	assert.Equal(t, "/path/to/key.pem", helmCreds.KeyFile)
	assert.Equal(t, "/path/to/keyring", helmCreds.Keyring)
}

func TestTypedCredentialResolution_DirectCredentialsFallback(t *testing.T) {
	// Config with old-style Credentials/v1 — should still work
	yaml := `
type: credentials.config.ocm.software/v1
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

	cfgScheme := runtime.NewScheme()
	credentialsv1.MustRegister(cfgScheme)

	var configv1 credentialsv1.Config
	require.NoError(t, cfgScheme.Decode(strings.NewReader(yaml), &configv1))
	config := credentialruntime.ConvertFromV1(&configv1)

	graph, err := credentials.ToGraph(t.Context(), config, credentials.Options{})
	require.NoError(t, err)

	identity := runtime.Identity{
		"type":     "HelmChartRepository",
		"hostname": "charts.example.com",
	}

	// ResolveTyped returns *DirectCredentials for old-style configs
	typed, err := graph.ResolveTyped(t.Context(), identity)
	require.NoError(t, err)

	direct, ok := typed.(*credentialsv1.DirectCredentials)
	require.True(t, ok, "expected *DirectCredentials, got %T", typed)
	assert.Equal(t, "legacyuser", direct.Properties["username"])
	assert.Equal(t, "legacypass", direct.Properties["password"])

	// Resolve (map) also works for backward compat
	credMap, err := graph.Resolve(t.Context(), identity)
	require.NoError(t, err)
	assert.Equal(t, "legacyuser", credMap["username"])
}

func TestTypedCredentialResolution_HelmHTTPCredentials_PartialFields(t *testing.T) {
	// Config with HelmHTTPCredentials/v1 but only certFile/keyFile (no username/password)
	yaml := `
type: credentials.config.ocm.software/v1
consumers:
  - identity:
      type: HelmChartRepository
      hostname: "charts.example.com"
    credentials:
      - type: HelmHTTPCredentials/v1
        certFile: "/path/to/cert.pem"
        keyFile: "/path/to/key.pem"
`

	cfgScheme := runtime.NewScheme()
	credentialsv1.MustRegister(cfgScheme)

	credTypeScheme := runtime.NewScheme()
	helmcredsv1.MustRegisterCredentialType(credTypeScheme)

	var configv1 credentialsv1.Config
	require.NoError(t, cfgScheme.Decode(strings.NewReader(yaml), &configv1))
	config := credentialruntime.ConvertFromV1(&configv1)

	graph, err := credentials.ToGraph(t.Context(), config, credentials.Options{
		CredentialTypeScheme: credTypeScheme,
	})
	require.NoError(t, err)

	identity := runtime.Identity{
		"type":     "HelmChartRepository",
		"hostname": "charts.example.com",
	}

	typed, err := graph.ResolveTyped(t.Context(), identity)
	require.NoError(t, err)

	helmCreds, ok := typed.(*helmcredsv1.HelmHTTPCredentials)
	require.True(t, ok, "expected *HelmHTTPCredentials, got %T", typed)

	// Only certFile/keyFile should be set
	assert.Empty(t, helmCreds.Username)
	assert.Empty(t, helmCreds.Password)
	assert.Equal(t, "/path/to/cert.pem", helmCreds.CertFile)
	assert.Equal(t, "/path/to/key.pem", helmCreds.KeyFile)
	assert.Empty(t, helmCreds.Keyring)
}

func TestTypedCredentialResolution_DirectCredentials_WrongPropertyNames(t *testing.T) {
	// Config with Credentials/v1 using wrong property names — should resolve
	// but fields won't map to HelmHTTPCredentials
	yaml := `
type: credentials.config.ocm.software/v1
consumers:
  - identity:
      type: HelmChartRepository
      hostname: "charts.example.com"
    credentials:
      - type: Credentials/v1
        properties:
          access_token: "some-token"
          refresh_token: "some-refresh"
          unknown_field: "some-value"
`

	cfgScheme := runtime.NewScheme()
	credentialsv1.MustRegister(cfgScheme)

	var configv1 credentialsv1.Config
	require.NoError(t, cfgScheme.Decode(strings.NewReader(yaml), &configv1))
	config := credentialruntime.ConvertFromV1(&configv1)

	graph, err := credentials.ToGraph(t.Context(), config, credentials.Options{})
	require.NoError(t, err)

	identity := runtime.Identity{
		"type":     "HelmChartRepository",
		"hostname": "charts.example.com",
	}

	// Graph stores as *DirectCredentials (no credential type scheme set)
	typed, err := graph.ResolveTyped(t.Context(), identity)
	require.NoError(t, err)

	direct, ok := typed.(*credentialsv1.DirectCredentials)
	require.True(t, ok, "expected *DirectCredentials, got %T", typed)

	// Properties are preserved as-is — the map has the wrong keys
	assert.Equal(t, "some-token", direct.Properties["access_token"])
	assert.Equal(t, "some-value", direct.Properties["unknown_field"])

	// Converting to HelmHTTPCredentials yields empty fields (property names don't match)
	helmCreds, err := toHelmHTTPCredentials(typed)
	require.NoError(t, err)
	require.NotNil(t, helmCreds) // FromDirectCredentials always succeeds
	assert.Empty(t, helmCreds.Username)
	assert.Empty(t, helmCreds.Password)
	assert.Empty(t, helmCreds.CertFile)
}

func TestTypedCredentialResolution_EmptyCredentials(t *testing.T) {
	// Config with HelmHTTPCredentials/v1 but no fields set at all
	yaml := `
type: credentials.config.ocm.software/v1
consumers:
  - identity:
      type: HelmChartRepository
      hostname: "charts.example.com"
    credentials:
      - type: HelmHTTPCredentials/v1
`

	cfgScheme := runtime.NewScheme()
	credentialsv1.MustRegister(cfgScheme)

	credTypeScheme := runtime.NewScheme()
	helmcredsv1.MustRegisterCredentialType(credTypeScheme)

	var configv1 credentialsv1.Config
	require.NoError(t, cfgScheme.Decode(strings.NewReader(yaml), &configv1))
	config := credentialruntime.ConvertFromV1(&configv1)

	graph, err := credentials.ToGraph(t.Context(), config, credentials.Options{
		CredentialTypeScheme: credTypeScheme,
	})
	require.NoError(t, err)

	identity := runtime.Identity{
		"type":     "HelmChartRepository",
		"hostname": "charts.example.com",
	}

	typed, err := graph.ResolveTyped(t.Context(), identity)
	require.NoError(t, err)

	helmCreds, ok := typed.(*helmcredsv1.HelmHTTPCredentials)
	require.True(t, ok, "expected *HelmHTTPCredentials, got %T", typed)

	// All fields empty — valid type but no properties
	assert.Empty(t, helmCreds.Username)
	assert.Empty(t, helmCreds.Password)
	assert.Empty(t, helmCreds.CertFile)
	assert.Empty(t, helmCreds.KeyFile)
	assert.Empty(t, helmCreds.Keyring)
}

func TestToHelmHTTPCredentials_TypeSwitch(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		result, err := toHelmHTTPCredentials(nil)
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("HelmHTTPCredentials passes through", func(t *testing.T) {
		creds := &helmcredsv1.HelmHTTPCredentials{
			Username: "user",
			CertFile: "/cert.pem",
		}
		result, err := toHelmHTTPCredentials(creds)
		require.NoError(t, err)
		assert.Equal(t, creds, result)
	})

	t.Run("DirectCredentials converts", func(t *testing.T) {
		direct := &credentialsv1.DirectCredentials{
			Properties: map[string]string{
				"username": "user",
				"certFile": "/cert.pem",
			},
		}
		result, err := toHelmHTTPCredentials(direct)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "user", result.Username)
		assert.Equal(t, "/cert.pem", result.CertFile)
	})

	t.Run("unknown type returns error", func(t *testing.T) {
		unknown := &runtime.Raw{Type: runtime.NewUnversionedType("RSACredentials")}
		result, err := toHelmHTTPCredentials(unknown)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "unsupported credential type")
		assert.Contains(t, err.Error(), "RSACredentials")
	})
}

func TestCredentialAcceptorValidation_WarnsOnMismatch(t *testing.T) {
	// Config with an incompatible credential type for HelmChartRepository identity
	yaml := `
type: credentials.config.ocm.software/v1
consumers:
  - identity:
      type: HelmChartRepository
      hostname: "charts.example.com"
    credentials:
      - type: RSACredentials/v1
        publicKeyPEM: "some-key"
`

	cfgScheme := runtime.NewScheme()
	credentialsv1.MustRegister(cfgScheme)

	// Register both identity and credential type schemes
	identityTypeScheme := runtime.NewScheme()
	helmidentityv1.MustRegisterIdentityType(identityTypeScheme)

	credTypeScheme := runtime.NewScheme()
	helmcredsv1.MustRegisterCredentialType(credTypeScheme)

	var configv1 credentialsv1.Config
	require.NoError(t, cfgScheme.Decode(strings.NewReader(yaml), &configv1))
	config := credentialruntime.ConvertFromV1(&configv1)

	// Capture log output
	var logBuf bytes.Buffer
	handler := slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	_, err := credentials.ToGraph(t.Context(), config, credentials.Options{
		ConsumerIdentityTypeScheme: identityTypeScheme,
		CredentialTypeScheme:       credTypeScheme,
		CredentialPluginProvider: credentials.GetCredentialPluginFn(func(ctx context.Context, typed runtime.Typed) (credentials.CredentialPlugin, error) {
			return nil, fmt.Errorf("no credential plugin for type %s", typed.GetType())
		}),
	})
	// Graph creation may fail because RSACredentials/v1 is unknown to both schemes
	// and the credential plugin provider rejects it. This is expected.
	// The important thing is the warning was logged BEFORE the error.
	_ = err

	// Verify the warning was logged
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "credential type not accepted by identity type",
		"should warn about mismatched credential type")
	assert.Contains(t, logOutput, "RSACredentials/v1",
		"warning should mention the incompatible credential type")
	assert.Contains(t, logOutput, "HelmChartRepository",
		"warning should mention the identity type")
}

func TestCredentialAcceptorValidation_NoWarningForValidType(t *testing.T) {
	// Config with the correct credential type for HelmChartRepository
	yaml := `
type: credentials.config.ocm.software/v1
consumers:
  - identity:
      type: HelmChartRepository
      hostname: "charts.example.com"
    credentials:
      - type: HelmHTTPCredentials/v1
        username: "user"
`

	cfgScheme := runtime.NewScheme()
	credentialsv1.MustRegister(cfgScheme)

	identityTypeScheme := runtime.NewScheme()
	helmidentityv1.MustRegisterIdentityType(identityTypeScheme)

	credTypeScheme := runtime.NewScheme()
	helmcredsv1.MustRegisterCredentialType(credTypeScheme)

	var configv1 credentialsv1.Config
	require.NoError(t, cfgScheme.Decode(strings.NewReader(yaml), &configv1))
	config := credentialruntime.ConvertFromV1(&configv1)

	// Capture log output
	var logBuf bytes.Buffer
	handler := slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	_, err := credentials.ToGraph(t.Context(), config, credentials.Options{
		ConsumerIdentityTypeScheme: identityTypeScheme,
		CredentialTypeScheme:       credTypeScheme,
	})
	require.NoError(t, err)

	logOutput := logBuf.String()
	assert.Empty(t, logOutput, "no warnings should be logged for valid credential type")
}

func TestCredentialAcceptorValidation_DirectCredentialsAlwaysAccepted(t *testing.T) {
	// DirectCredentials should never trigger a warning, even with CredentialAcceptor
	yaml := `
type: credentials.config.ocm.software/v1
consumers:
  - identity:
      type: HelmChartRepository
      hostname: "charts.example.com"
    credentials:
      - type: Credentials/v1
        properties:
          username: "user"
`

	cfgScheme := runtime.NewScheme()
	credentialsv1.MustRegister(cfgScheme)

	identityTypeScheme := runtime.NewScheme()
	helmidentityv1.MustRegisterIdentityType(identityTypeScheme)

	var configv1 credentialsv1.Config
	require.NoError(t, cfgScheme.Decode(strings.NewReader(yaml), &configv1))
	config := credentialruntime.ConvertFromV1(&configv1)

	var logBuf bytes.Buffer
	handler := slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	_, err := credentials.ToGraph(t.Context(), config, credentials.Options{
		ConsumerIdentityTypeScheme: identityTypeScheme,
	})
	require.NoError(t, err)

	logOutput := logBuf.String()
	assert.Empty(t, logOutput, "DirectCredentials should never trigger credential type warnings")
}
