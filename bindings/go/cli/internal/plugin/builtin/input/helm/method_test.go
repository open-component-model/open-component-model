package helm

import (
	"testing"

	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	helmv1 "ocm.software/open-component-model/bindings/go/helm/spec/input/v1"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialtyperepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestRegister(t *testing.T) {
	ctx := t.Context()
	registry := input.NewInputRepositoryRegistry(ctx)
	credentialTypeRegistry := credentialtyperepository.NewCredentialTypeRegistry(ctx)
	tempFolder := t.TempDir()
	cfg := &filesystemv1alpha1.Config{
		TempFolder: &tempFolder,
	}

	require.NoError(t, Register(registry, credentialTypeRegistry, cfg, &httpv1alpha1.Config{}))

	helmSpec := &helmv1.Helm{
		Type: runtime.NewVersionedType(helmv1.Type, helmv1.Version),
		Path: "/some/chart",
	}
	plugin, err := registry.GetResourceInputPlugin(ctx, helmSpec)
	require.NoError(t, err)
	require.NotNil(t, plugin)
}
