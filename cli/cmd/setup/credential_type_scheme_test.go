package setup_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	rsacredsv1 "ocm.software/open-component-model/bindings/go/rsa/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/plugin/builtin"
)

// TestCredentialTypeSchemePopulatedByBuiltinRegister verifies that calling builtin.Register
// populates PluginManager.CredentialTypeScheme with the typed consumer credential structs
// declared by each built-in binding (ADR 0021 §Type Registries and Graph Independence).
//
// Before this wiring existed, CredentialTypeScheme was always nil/empty and the credential
// graph would fall back to *DirectCredentials for every credential type, including
// OCICredentials/v1, HelmHTTPCredentials/v1, and RSACredentials/v1.
func TestCredentialTypeSchemePopulatedByBuiltinRegister(t *testing.T) {
	r := require.New(t)

	pm := manager.NewPluginManager(context.Background())
	r.NoError(builtin.Register(pm, &filesystemv1alpha1.Config{}, slog.Default()))

	scheme := pm.CredentialTypeRegistry.GetCredentialTypeScheme()
	r.NotNil(scheme, "CredentialTypeRegistry scheme must not be nil after builtin.Register")

	r.True(scheme.IsRegistered(runtime.NewVersionedType(ocicredsv1.OCICredentialsType, ocicredsv1.Version)),
		"OCICredentials/v1 must be registered in the credential type scheme")

	r.True(scheme.IsRegistered(runtime.NewVersionedType(helmcredsv1.HelmHTTPCredentialsType, helmcredsv1.Version)),
		"HelmHTTPCredentials/v1 must be registered in the credential type scheme")

	r.True(scheme.IsRegistered(rsacredsv1.VersionedType),
		"RSACredentials/v1 must be registered in the credential type scheme")
}
