package helm

import (
	"testing"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	helmv1 "ocm.software/open-component-model/bindings/go/helm/input/spec/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
	"ocm.software/open-component-model/bindings/go/runtime"

	"github.com/stretchr/testify/require"
)

func TestRegister(t *testing.T) {
	ctx := t.Context()
	registry := input.NewInputRepositoryRegistry(ctx)
	cfg := &filesystemv1alpha1.Config{
		TempFolder: t.TempDir(),
	}

	require.NoError(t, Register(registry, cfg))

	helmSpec := &helmv1.Helm{
		Type: runtime.NewVersionedType(helmv1.Type, helmv1.Version),
		Path: "/some/chart",
	}
	plugin, err := registry.GetResourceInputPlugin(ctx, helmSpec)
	require.NoError(t, err)
	require.NotNil(t, plugin)
}
