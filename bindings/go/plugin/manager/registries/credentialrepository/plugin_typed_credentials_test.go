package credentialrepository

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
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"

	credcapv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// VaultCredentials is a typed credential struct that a consumer defines
// to work with a plugin's credential type.
type VaultCredentials struct {
	Type     runtime.Type `json:"type"`
	RoleID   string       `json:"role_id"`
	SecretID string       `json:"secret_id"`
}

func (v *VaultCredentials) GetType() runtime.Type        { return v.Type }
func (v *VaultCredentials) SetType(t runtime.Type)       { v.Type = t }
func (v *VaultCredentials) DeepCopyTyped() runtime.Typed { cp := *v; return &cp }

// VaultIdentity is a typed identity struct for validation — implements CredentialAcceptor
// based on the plugin's capability declaration.
type VaultIdentity struct {
	Type     runtime.Type `json:"type"`
	Hostname string       `json:"hostname,omitempty"`

	// acceptedTypes is populated from the plugin's capability spec
	acceptedTypes []runtime.Type
}

func (v *VaultIdentity) GetType() runtime.Type        { return v.Type }
func (v *VaultIdentity) SetType(t runtime.Type)       { v.Type = t }
func (v *VaultIdentity) DeepCopyTyped() runtime.Typed { cp := *v; return &cp }
func (v *VaultIdentity) AcceptedCredentialTypes() []runtime.Type {
	return v.acceptedTypes
}

var _ runtime.CredentialAcceptor = (*VaultIdentity)(nil)

// buildIdentitySchemeFromCapabilities shows how the composition root builds an identity
// type scheme from plugin capabilities, wiring AcceptedCredentialTypes from the capability JSON.
func buildIdentitySchemeFromCapabilities(capabilities []credcapv1.CapabilitySpec) *runtime.Scheme {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	for _, cap := range capabilities {
		for _, idType := range cap.SupportedConsumerIdentityTypes {
			// Create a VaultIdentity prototype with accepted types from the capability
			proto := &VaultIdentity{
				acceptedTypes: idType.AcceptedCredentialTypes,
			}
			_ = scheme.RegisterWithAlias(proto, idType.Type)
			for _, alias := range idType.Aliases {
				_ = scheme.RegisterWithAlias(proto, alias)
			}
		}
	}
	return scheme
}

// TestPluginCapabilityDrivenSchemeRegistration demonstrates the full flow:
// plugin declares identity types with accepted credential types, composition root
// builds schemes, graph resolves and validates.
func TestPluginCapabilityDrivenSchemeRegistration(t *testing.T) {
	ctx := t.Context()

	// 1. Plugin capability with AcceptedCredentialTypes on identity
	pluginCapabilities := []credcapv1.CapabilitySpec{
		{
			Type: runtime.NewUnversionedType(string(credcapv1.CredentialRepositoryPluginType)),
			SupportedConsumerIdentityTypes: []mtypes.Type{
				{
					Type: runtime.NewVersionedType("VaultServer", "v1"),
					AcceptedCredentialTypes: []runtime.Type{
						runtime.NewVersionedType("VaultCredentials", "v1"),
					},
				},
			},
		},
	}

	// 2. Consumer registers Go struct for the credential type
	credentialTypeScheme := runtime.NewScheme()
	credentialTypeScheme.MustRegisterWithAlias(&VaultCredentials{},
		runtime.NewVersionedType("VaultCredentials", "v1"),
	)

	// 3. Build identity scheme from capabilities (wires AcceptedCredentialTypes)
	identityTypeScheme := buildIdentitySchemeFromCapabilities(pluginCapabilities)

	// 4. Config with the plugin's credential type
	yaml := `
type: credentials.config.ocm.software/v1
consumers:
  - identity:
      type: VaultServer/v1
      hostname: "vault.example.com"
    credentials:
      - type: VaultCredentials/v1
        role_id: "my-role"
        secret_id: "my-secret"
`

	cfgScheme := runtime.NewScheme()
	credentialsv1.MustRegister(cfgScheme)

	var configv1 credentialsv1.Config
	require.NoError(t, cfgScheme.Decode(strings.NewReader(yaml), &configv1))
	config := credentialruntime.ConvertFromV1(&configv1)

	// 5. Graph with both schemes
	graph, err := credentials.ToGraph(ctx, config, credentials.Options{
		CredentialTypeSchemeProvider:       &credentials.SchemeAsCredentialTypeSchemeProvider{S: credentialTypeScheme},
		IdentityTypeSchemeProvider: &credentials.SchemeAsIdentityTypeSchemeProvider{S: identityTypeScheme},
	})
	require.NoError(t, err)

	// 6. Resolve — direct type assertion
	identity := runtime.Identity{
		"type":     "VaultServer/v1",
		"hostname": "vault.example.com",
	}

	resolved, err := graph.ResolveTyped(ctx, identity)
	require.NoError(t, err)

	vaultCreds, ok := resolved.(*VaultCredentials)
	require.True(t, ok, "expected *VaultCredentials, got %T", resolved)
	assert.Equal(t, "my-role", vaultCreds.RoleID)
	assert.Equal(t, "my-secret", vaultCreds.SecretID)
}

// TestPluginCapability_AcceptedCredentialTypes_WarnsOnMismatch verifies that the graph
// warns when a config has a credential type not accepted by the identity's capability.
func TestPluginCapability_AcceptedCredentialTypes_WarnsOnMismatch(t *testing.T) {
	ctx := t.Context()

	// Plugin's VaultServer identity only accepts VaultCredentials
	pluginCapabilities := []credcapv1.CapabilitySpec{
		{
			Type: runtime.NewUnversionedType(string(credcapv1.CredentialRepositoryPluginType)),
			SupportedConsumerIdentityTypes: []mtypes.Type{
				{
					Type: runtime.NewVersionedType("VaultServer", "v1"),
					AcceptedCredentialTypes: []runtime.Type{
						runtime.NewVersionedType("VaultCredentials", "v1"),
					},
				},
			},
		},
	}

	identityTypeScheme := buildIdentitySchemeFromCapabilities(pluginCapabilities)

	// Config puts RSACredentials on a VaultServer identity — mismatch!
	yaml := `
type: credentials.config.ocm.software/v1
consumers:
  - identity:
      type: VaultServer/v1
      hostname: "vault.example.com"
    credentials:
      - type: WrongCredentials/v1
        someField: "value"
`

	cfgScheme := runtime.NewScheme()
	credentialsv1.MustRegister(cfgScheme)

	var configv1 credentialsv1.Config
	require.NoError(t, cfgScheme.Decode(strings.NewReader(yaml), &configv1))
	config := credentialruntime.ConvertFromV1(&configv1)

	// Capture warnings
	var logBuf bytes.Buffer
	handler := slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	_, _ = credentials.ToGraph(ctx, config, credentials.Options{
		IdentityTypeSchemeProvider: &credentials.SchemeAsIdentityTypeSchemeProvider{S: identityTypeScheme},
		CredentialPluginProvider: credentials.GetCredentialPluginFn(func(ctx context.Context, typed runtime.Typed) (credentials.CredentialPlugin, error) {
			return nil, fmt.Errorf("no plugin for %s", typed.GetType())
		}),
	})

	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "credential type not accepted by identity type",
		"should warn about mismatched credential type")
	assert.Contains(t, logOutput, "WrongCredentials/v1")
	assert.Contains(t, logOutput, "VaultServer/v1")
}

// TestPluginCapability_AcceptedCredentialTypes_NoWarningForValid verifies no warning
// when the credential type matches what the identity accepts.
func TestPluginCapability_AcceptedCredentialTypes_NoWarningForValid(t *testing.T) {
	ctx := t.Context()

	pluginCapabilities := []credcapv1.CapabilitySpec{
		{
			Type: runtime.NewUnversionedType(string(credcapv1.CredentialRepositoryPluginType)),
			SupportedConsumerIdentityTypes: []mtypes.Type{
				{
					Type: runtime.NewVersionedType("VaultServer", "v1"),
					AcceptedCredentialTypes: []runtime.Type{
						runtime.NewVersionedType("VaultCredentials", "v1"),
					},
				},
			},
		},
	}

	identityTypeScheme := buildIdentitySchemeFromCapabilities(pluginCapabilities)

	credentialTypeScheme := runtime.NewScheme()
	credentialTypeScheme.MustRegisterWithAlias(&VaultCredentials{},
		runtime.NewVersionedType("VaultCredentials", "v1"),
	)

	yaml := `
type: credentials.config.ocm.software/v1
consumers:
  - identity:
      type: VaultServer/v1
      hostname: "vault.example.com"
    credentials:
      - type: VaultCredentials/v1
        role_id: "ok"
`

	cfgScheme := runtime.NewScheme()
	credentialsv1.MustRegister(cfgScheme)

	var configv1 credentialsv1.Config
	require.NoError(t, cfgScheme.Decode(strings.NewReader(yaml), &configv1))
	config := credentialruntime.ConvertFromV1(&configv1)

	var logBuf bytes.Buffer
	handler := slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	_, err := credentials.ToGraph(ctx, config, credentials.Options{
		CredentialTypeSchemeProvider:       &credentials.SchemeAsCredentialTypeSchemeProvider{S: credentialTypeScheme},
		IdentityTypeSchemeProvider: &credentials.SchemeAsIdentityTypeSchemeProvider{S: identityTypeScheme},
	})
	require.NoError(t, err)

	assert.Empty(t, logBuf.String(), "no warnings for valid credential type")
}

// TestPluginCredentialWithSchemeConvert demonstrates converting from Raw to typed struct.
func TestPluginCredentialWithSchemeConvert(t *testing.T) {
	ctx := t.Context()

	credentialTypeScheme := runtime.NewScheme(runtime.WithAllowUnknown())

	yaml := `
type: credentials.config.ocm.software/v1
consumers:
  - identity:
      type: VaultServer/v1
      hostname: "vault.example.com"
    credentials:
      - type: VaultCredentials/v1
        role_id: "convert-role"
        secret_id: "convert-secret"
`

	cfgScheme := runtime.NewScheme()
	credentialsv1.MustRegister(cfgScheme)

	var configv1 credentialsv1.Config
	require.NoError(t, cfgScheme.Decode(strings.NewReader(yaml), &configv1))
	config := credentialruntime.ConvertFromV1(&configv1)

	graph, err := credentials.ToGraph(ctx, config, credentials.Options{
		CredentialTypeSchemeProvider: &credentials.SchemeAsCredentialTypeSchemeProvider{S: credentialTypeScheme},
	})
	require.NoError(t, err)

	identity := runtime.Identity{
		"type":     "VaultServer/v1",
		"hostname": "vault.example.com",
	}

	resolved, err := graph.ResolveTyped(ctx, identity)
	require.NoError(t, err)

	// Convert Raw → typed struct
	var vaultCreds VaultCredentials
	convertScheme := runtime.NewScheme()
	convertScheme.MustRegisterWithAlias(&VaultCredentials{},
		runtime.NewVersionedType("VaultCredentials", "v1"),
	)
	require.NoError(t, convertScheme.Convert(resolved, &vaultCreds))

	assert.Equal(t, "convert-role", vaultCreds.RoleID)
	assert.Equal(t, "convert-secret", vaultCreds.SecretID)
}
