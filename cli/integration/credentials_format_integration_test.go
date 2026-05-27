package integration

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	credconfigruntime "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	credv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd/configuration"
	"ocm.software/open-component-model/cli/integration/internal"
	"ocm.software/open-component-model/cli/internal/plugin/builtin"
)

func buildCredentialGraph(ctx context.Context, t *testing.T, cfgPath string, withTypedScheme bool) credentials.Resolver {
	t.Helper()
	r := require.New(t)

	ocmconf, err := configuration.GetConfigFromPath(cfgPath)
	r.NoError(err)
	credconf, err := credconfigruntime.LookupCredentialConfig(ocmconf)
	r.NoError(err)

	opts := credentials.Options{
		RepositoryPluginProvider: credentials.GetRepositoryPluginFn(func(_ context.Context, typed ocmruntime.Typed) (credentials.RepositoryPlugin, error) {
			return nil, fmt.Errorf("no repository plugin for type %s", typed.GetType())
		}),
		CredentialPluginProvider: credentials.GetCredentialPluginFn(func(_ context.Context, typed ocmruntime.Typed) (credentials.CredentialPlugin, error) {
			return nil, fmt.Errorf("no credential plugin for type %s", typed.GetType())
		}),
		CredentialRepositoryTypeScheme: ocmruntime.NewScheme(),
	}
	if withTypedScheme {
		pm := manager.NewPluginManager(ctx)
		r.NoError(builtin.Register(pm, &filesystemv1alpha1.Config{}, slog.Default()))
		opts.CredentialTypeSchemeProvider = pm.CredentialTypeRegistry
	}

	graph, err := credentials.ToGraph(ctx, credconf, opts)
	r.NoError(err)
	return graph
}

func ociIdentity(host, port string) ocmruntime.Identity {
	return ocmruntime.Identity{"type": "OCIRegistry", "hostname": host, "port": port, "scheme": "http"}
}

// Test_Credentials_OldFormat_DirectCredentials checks that the legacy Credentials/v1
// properties-map format resolves with and without CredentialTypeSchemeProvider.
func Test_Credentials_OldFormat_DirectCredentials(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := t.Context()

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	cfg := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRegistry
      hostname: %[1]q
      port: %[2]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %[3]q
        password: %[4]q
`, registry.Host, registry.Port, registry.User, registry.Password)

	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))
	identity := ociIdentity(registry.Host, registry.Port)

	t.Run("without CredentialTypeSchemeProvider", func(t *testing.T) {
		r := require.New(t)
		resolved, err := buildCredentialGraph(ctx, t, cfgPath, false).Resolve(ctx, identity)
		r.NoError(err)
		dc, ok := resolved.(*credv1.DirectCredentials)
		r.True(ok, "expected *DirectCredentials, got %T", resolved)
		r.Equal(registry.User, dc.Properties["username"])
		r.Equal(registry.Password, dc.Properties["password"])
	})

	t.Run("with CredentialTypeSchemeProvider", func(t *testing.T) {
		r := require.New(t)
		resolved, err := buildCredentialGraph(ctx, t, cfgPath, true).Resolve(ctx, identity)
		r.NoError(err)
		// Credentials/v1 is not a typed struct in the OCI scheme — still comes back as DirectCredentials.
		_, ok := resolved.(*credv1.DirectCredentials)
		r.True(ok, "expected *DirectCredentials for old format, got %T", resolved)
	})
}

// Test_Credentials_NewFormat_OCICredentials checks that OCICredentials/v1 resolves to
// *OCICredentials with the scheme wired, and falls back to *DirectCredentials without it.
func Test_Credentials_NewFormat_OCICredentials(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := t.Context()

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	cfg := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRegistry
      hostname: %[1]q
      port: %[2]q
      scheme: http
    credentials:
    - type: OCICredentials/v1
      username: %[3]q
      password: %[4]q
`, registry.Host, registry.Port, registry.User, registry.Password)

	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))
	identity := ociIdentity(registry.Host, registry.Port)

	t.Run("without CredentialTypeSchemeProvider — graph construction fails", func(t *testing.T) {
		r := require.New(t)
		// Without the scheme, OCICredentials/v1 is treated as an unknown plugin-backed
		// credential type. ToGraph errors at ingest time because no credential plugin
		// is configured for that type.
		ocmconf, err := configuration.GetConfigFromPath(cfgPath)
		r.NoError(err)
		credconf, err := credconfigruntime.LookupCredentialConfig(ocmconf)
		r.NoError(err)
		_, err = credentials.ToGraph(ctx, credconf, credentials.Options{
			RepositoryPluginProvider: credentials.GetRepositoryPluginFn(func(_ context.Context, typed ocmruntime.Typed) (credentials.RepositoryPlugin, error) {
				return nil, fmt.Errorf("no repository plugin for type %s", typed.GetType())
			}),
			CredentialPluginProvider: credentials.GetCredentialPluginFn(func(_ context.Context, typed ocmruntime.Typed) (credentials.CredentialPlugin, error) {
				return nil, fmt.Errorf("no credential plugin for type %s", typed.GetType())
			}),
			CredentialRepositoryTypeScheme: ocmruntime.NewScheme(),
		})
		r.ErrorContains(err, "OCICredentials/v1",
			"without CredentialTypeSchemeProvider, OCICredentials/v1 must not be silently dropped")
	})

	t.Run("with CredentialTypeSchemeProvider — resolves to *OCICredentials", func(t *testing.T) {
		r := require.New(t)
		resolved, err := buildCredentialGraph(ctx, t, cfgPath, true).Resolve(ctx, identity)
		r.NoError(err)
		ociCreds, ok := resolved.(*ocicredsv1.OCICredentials)
		r.True(ok, "with scheme provider expected *OCICredentials, got %T", resolved)
		r.Equal(registry.User, ociCreds.Username)
		r.Equal(registry.Password, ociCreds.Password)
	})
}
