package setup_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	credconfigruntime "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	gpgcredsv1alpha1 "ocm.software/open-component-model/bindings/go/gpg/spec/credentials/v1alpha1"
	gpgidentityv1alpha1 "ocm.software/open-component-model/bindings/go/gpg/spec/identity/v1alpha1"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	credidentityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	rsacredsv1 "ocm.software/open-component-model/bindings/go/rsa/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/plugin/builtin"
)

// TestCredentialTypeSchemePopulatedByBuiltinRegister verifies that calling builtin.Register
// populates the credential type scheme inside PluginManager.CredentialRepositoryRegistry with
// the typed consumer credential structs declared by each built-in binding
// (ADR 0021 §Type Registries and Graph Independence).
//
// Before this wiring existed, the credential type scheme was always empty and the credential
// graph would fall back to *DirectCredentials for every credential type, including
// OCICredentials/v1, HelmHTTPCredentials/v1, and RSACredentials/v1.
func TestCredentialTypeSchemePopulatedByBuiltinRegister(t *testing.T) {
	r := require.New(t)

	pm := manager.NewPluginManager(context.Background())
	r.NoError(builtin.Register(pm, &filesystemv1alpha1.Config{}, slog.Default()))

	scheme := pm.CredentialRepositoryRegistry.GetCredentialTypeScheme()
	r.NotNil(scheme, "CredentialRepositoryRegistry credential type scheme must not be nil after builtin.Register")

	r.True(scheme.IsRegistered(runtime.NewVersionedType(ocicredsv1.OCICredentialsType, ocicredsv1.Version)),
		"OCICredentials/v1 must be registered in the credential type scheme")

	r.True(scheme.IsRegistered(runtime.NewVersionedType(helmcredsv1.HelmHTTPCredentialsType, helmcredsv1.Version)),
		"HelmHTTPCredentials/v1 must be registered in the credential type scheme")

	r.True(scheme.IsRegistered(rsacredsv1.VersionedType),
		"RSACredentials/v1 must be registered in the credential type scheme")

	r.True(scheme.IsRegistered(runtime.NewVersionedType(gpgcredsv1alpha1.GPGCredentialsType, gpgcredsv1alpha1.Version)),
		"GPGCredentials/v1alpha1 must be registered in the credential type scheme")
}

// TestCredentialGraphResolvesTypedCredentials verifies that a credential graph built with
// PluginManager.CredentialRepositoryRegistry as the scheme provider resolves each built-in
// typed credential format to its concrete Go type instead of falling back to *DirectCredentials.
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
			name: "GPGCredentials/v1alpha1",
			identity: runtime.Identity{
				"type":      gpgidentityv1alpha1.V1Alpha1Type.String(),
				"signature": "default",
			},
			credential: &gpgcredsv1alpha1.GPGCredentials{
				Type:          runtime.NewVersionedType(gpgcredsv1alpha1.GPGCredentialsType, gpgcredsv1alpha1.Version),
				PrivateKeyPGP: "placeholder-key",
				PublicKeyPGP:  "placeholder-key",
			},
			assertType: func(t *testing.T, resolved runtime.Typed) {
				t.Helper()
				creds, ok := resolved.(*gpgcredsv1alpha1.GPGCredentials)
				require.True(t, ok, "expected *GPGCredentials, got %T", resolved)
				require.Equal(t, "placeholder-key", creds.PrivateKeyPGP)
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
