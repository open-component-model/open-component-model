package setup_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	credconfigruntime "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	credv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	gpgcredsv1alpha1 "ocm.software/open-component-model/bindings/go/gpg/spec/credentials/v1alpha1"
	gpgidentityv1alpha1 "ocm.software/open-component-model/bindings/go/gpg/spec/identity/v1alpha1"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	helmidentityv1 "ocm.software/open-component-model/bindings/go/helm/spec/identity/v1"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	credidentityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	rsacredsv1 "ocm.software/open-component-model/bindings/go/rsa/spec/credentials/v1"
	rsaidentityv1 "ocm.software/open-component-model/bindings/go/rsa/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	oidctokenv1alpha1 "ocm.software/open-component-model/bindings/go/sigstore/spec/credentials/oidcidentitytoken/v1alpha1"
	trustedrootv1alpha1 "ocm.software/open-component-model/bindings/go/sigstore/spec/credentials/trustedroot/v1alpha1"
	sigstoresignerv1alpha1 "ocm.software/open-component-model/bindings/go/sigstore/spec/identity/signer/v1alpha1"
	sigstoreverifierv1alpha1 "ocm.software/open-component-model/bindings/go/sigstore/spec/identity/verifier/v1alpha1"
	"ocm.software/open-component-model/cli/internal/plugin/builtin"
)

// TestCredentialTypeSchemePopulatedByBuiltinRegister verifies that calling builtin.Register
// populates the credential type scheme inside PluginManager.CredentialRepositoryRegistry with
// the typed consumer credential structs declared by each built-in binding
func TestCredentialTypeSchemePopulatedByBuiltinRegister(t *testing.T) {
	pm := manager.NewPluginManager(context.Background())
	require.NoError(t, builtin.Register(pm, &filesystemv1alpha1.Config{}, slog.Default()))

	scheme := pm.CredentialRepositoryRegistry.GetCredentialTypeScheme()
	require.NotNil(t, scheme)

	tests := []struct {
		name          string
		versionedType runtime.Type
	}{
		{"OCICredentials/v1", runtime.NewVersionedType(ocicredsv1.OCICredentialsType, ocicredsv1.Version)},
		{"HelmHTTPCredentials/v1", runtime.NewVersionedType(helmcredsv1.HelmHTTPCredentialsType, helmcredsv1.Version)},
		{"RSACredentials/v1", rsacredsv1.VersionedType},
		{"GPGCredentials/v1alpha1", runtime.NewVersionedType(gpgcredsv1alpha1.GPGCredentialsType, gpgcredsv1alpha1.Version)},
		{"OIDCIdentityToken/v1alpha1", oidctokenv1alpha1.VersionedType},
		{"TrustedRoot/v1alpha1", trustedrootv1alpha1.VersionedType},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.True(t, scheme.IsRegistered(tc.versionedType),
				"%s must be registered in the credential type scheme", tc.name)
		})
	}
}

// TestCredentialGraphResolvesTypedCredentials verifies that a credential graph built with
// PluginManager.CredentialRepositoryRegistry as the scheme provider resolves each built-in
// typed credential to its concrete Go type.
func TestCredentialGraphResolvesTypedCredentials(t *testing.T) {
	ctx := t.Context()

	pm := manager.NewPluginManager(ctx)
	require.NoError(t, builtin.Register(pm, &filesystemv1alpha1.Config{}, slog.Default()))

	tests := []struct {
		name       string
		identity   runtime.Identity
		credential runtime.Typed
		assertType func(t *testing.T, resolved runtime.Typed)
	}{
		{
			name: "OCICredentials/v1",
			identity: runtime.Identity{
				"type":     credidentityv1.Type.String(),
				"hostname": "ghcr.io",
			},
			credential: &ocicredsv1.OCICredentials{
				Type:     runtime.NewVersionedType(ocicredsv1.OCICredentialsType, ocicredsv1.Version),
				Username: "myuser",
				Password: "mypass",
			},
			assertType: func(t *testing.T, resolved runtime.Typed) {
				t.Helper()
				creds, ok := resolved.(*ocicredsv1.OCICredentials)
				require.True(t, ok, "expected *OCICredentials, got %T", resolved)
				require.Equal(t, "myuser", creds.Username)
				require.Equal(t, "mypass", creds.Password)
			},
		},
		{
			name: "HelmHTTPCredentials/v1",
			identity: runtime.Identity{
				"type":     helmidentityv1.VersionedType.String(),
				"hostname": "charts.example.com",
			},
			credential: &helmcredsv1.HelmHTTPCredentials{
				Type:     runtime.NewVersionedType(helmcredsv1.HelmHTTPCredentialsType, helmcredsv1.Version),
				Username: "helmuser",
				Password: "helmpass",
			},
			assertType: func(t *testing.T, resolved runtime.Typed) {
				t.Helper()
				creds, ok := resolved.(*helmcredsv1.HelmHTTPCredentials)
				require.True(t, ok, "expected *HelmHTTPCredentials, got %T", resolved)
				require.Equal(t, "helmuser", creds.Username)
			},
		},
		{
			name: "RSACredentials/v1",
			identity: runtime.Identity{
				"type":      rsaidentityv1.V1Alpha1Type.String(),
				"algorithm": "RSASSA-PSS",
				"signature": "default",
			},
			credential: &rsacredsv1.RSACredentials{
				Type:          rsacredsv1.VersionedType,
				PrivateKeyPEM: "placeholder",
				PublicKeyPEM:  "placeholder",
			},
			assertType: func(t *testing.T, resolved runtime.Typed) {
				t.Helper()
				creds, ok := resolved.(*rsacredsv1.RSACredentials)
				require.True(t, ok, "expected *RSACredentials, got %T", resolved)
				require.Equal(t, "placeholder", creds.PrivateKeyPEM)
			},
		},
		{
			name: "GPGCredentials/v1alpha1",
			identity: runtime.Identity{
				"type":      gpgidentityv1alpha1.V1Alpha1Type.String(),
				"signature": "default",
			},
			credential: &gpgcredsv1alpha1.GPGCredentials{
				Type:          runtime.NewVersionedType(gpgcredsv1alpha1.GPGCredentialsType, gpgcredsv1alpha1.Version),
				PrivateKeyPGP: "placeholder",
				PublicKeyPGP:  "placeholder",
			},
			assertType: func(t *testing.T, resolved runtime.Typed) {
				t.Helper()
				creds, ok := resolved.(*gpgcredsv1alpha1.GPGCredentials)
				require.True(t, ok, "expected *GPGCredentials, got %T", resolved)
				require.Equal(t, "placeholder", creds.PrivateKeyPGP)
			},
		},
		{
			name: "OIDCIdentityToken/v1alpha1",
			identity: runtime.Identity{
				"type": sigstoresignerv1alpha1.VersionedType.String(),
			},
			credential: &oidctokenv1alpha1.OIDCIdentityToken{
				Type:  oidctokenv1alpha1.VersionedType,
				Token: "placeholder-token",
			},
			assertType: func(t *testing.T, resolved runtime.Typed) {
				t.Helper()
				creds, ok := resolved.(*oidctokenv1alpha1.OIDCIdentityToken)
				require.True(t, ok, "expected *OIDCIdentityToken, got %T", resolved)
				require.Equal(t, "placeholder-token", creds.Token)
			},
		},
		{
			name: "TrustedRoot/v1alpha1",
			identity: runtime.Identity{
				"type": sigstoreverifierv1alpha1.VersionedType.String(),
			},
			credential: &trustedrootv1alpha1.TrustedRoot{
				Type:            trustedrootv1alpha1.VersionedType,
				TrustedRootJSON: "{}",
			},
			assertType: func(t *testing.T, resolved runtime.Typed) {
				t.Helper()
				creds, ok := resolved.(*trustedrootv1alpha1.TrustedRoot)
				require.True(t, ok, "expected *TrustedRoot, got %T", resolved)
				require.Equal(t, "{}", creds.TrustedRootJSON)
			},
		},
		{
			name: "Credentials/v1 falls back to DirectCredentials",
			identity: runtime.Identity{
				"type":     credidentityv1.Type.String(),
				"hostname": "legacy.example.com",
			},
			credential: &credv1.DirectCredentials{
				Type:       runtime.NewVersionedType("Credentials", "v1"),
				Properties: map[string]string{"username": "legacyuser", "password": "legacypass"},
			},
			assertType: func(t *testing.T, resolved runtime.Typed) {
				t.Helper()
				creds, ok := resolved.(*credv1.DirectCredentials)
				require.True(t, ok, "expected *DirectCredentials, got %T", resolved)
				require.Equal(t, "legacyuser", creds.Properties["username"])
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &credconfigruntime.Config{
				Consumers: []credconfigruntime.Consumer{
					{Identities: []runtime.Identity{tc.identity}, Credentials: []runtime.Typed{tc.credential}},
				},
			}

			graph, err := credentials.ToGraph(ctx, cfg, credentials.Options{
				CredentialTypeSchemeProvider: pm.CredentialRepositoryRegistry,
			})
			require.NoError(t, err)

			resolved, err := graph.Resolve(ctx, tc.identity)
			require.NoError(t, err)
			require.NotNil(t, resolved)

			tc.assertType(t, resolved)
		})
	}
}
