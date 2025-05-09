package componentversionrepository

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"

	repov1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestPluginFlow(t *testing.T) {
	path := filepath.Join("..", "..", "..", "tmp", "testdata", "test-plugin")
	_, err := os.Stat(path)
	require.NoError(t, err, "test plugin not found, please build the plugin under plugin/testplugin first")

	ctx := context.Background()
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	registry := NewComponentVersionRepositoryRegistry(ctx)
	config := mtypes.Config{
		ID:         "test-plugin-1",
		Type:       mtypes.Socket,
		PluginType: mtypes.ComponentVersionRepositoryPluginType,
	}
	serialized, err := json.Marshal(config)
	require.NoError(t, err)

	proto := &dummyv1.Repository{}
	typ, err := scheme.TypeForPrototype(proto)
	require.NoError(t, err)

	pluginCmd := exec.CommandContext(ctx, path, "--config", string(serialized))
	t.Cleanup(func() {
		_ = pluginCmd.Process.Kill()
		_ = os.Remove("/tmp/test-plugin-1-plugin.socket")
	})
	pipe, err := pluginCmd.StdoutPipe()
	require.NoError(t, err)
	plugin := mtypes.Plugin{
		ID:   "test-plugin-1",
		Path: path,
		Config: mtypes.Config{
			ID:         "test-plugin-1",
			Type:       mtypes.Socket,
			PluginType: mtypes.ComponentVersionRepositoryPluginType,
		},
		Types: map[mtypes.PluginType][]mtypes.Type{
			mtypes.ComponentVersionRepositoryPluginType: {
				{
					Type:       typ,
					JSONSchema: []byte(`{}`),
				},
			},
		},
		Cmd:    pluginCmd,
		Stdout: pipe,
	}
	require.NoError(t, registry.AddPlugin(plugin, typ))

	retrievedPlugin, err := GetReadWriteComponentVersionRepositoryPluginForType(ctx, registry, proto, scheme)
	require.NoError(t, err)
	desc, err := retrievedPlugin.GetComponentVersion(ctx, repov1.GetComponentVersionRequest[*dummyv1.Repository]{
		Repository: &dummyv1.Repository{
			Type: runtime.Type{
				Name:    "DummyRepository",
				Version: "v1",
			},
			BaseUrl: "base-url",
		},
		Name:    "test-component",
		Version: "1.0.0",
	}, map[string]string{})
	require.NoError(t, err)
	require.Equal(t, "test-component:1.0.0", desc.String())
}

func TestPluginNotFound(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	registry := NewComponentVersionRepositoryRegistry(ctx)
	proto := &dummyv1.Repository{}
	_, err := GetReadWriteComponentVersionRepositoryPluginForType(ctx, registry, proto, scheme)
	require.ErrorContains(t, err, "failed to get plugin for typ runtime.Type DummyRepository/v1: no plugin registered for type DummyRepository/v1")
}

func TestSchemeDoesNotExist(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	registry := NewComponentVersionRepositoryRegistry(ctx)
	proto := &dummyv1.Repository{}
	_, err := GetReadWriteComponentVersionRepositoryPluginForType(ctx, registry, proto, scheme)
	require.ErrorContains(t, err, "failed to get type for prototype *v1.Repository: prototype not found in registry")
}

func TestInternalPluginRegistry(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	registry := NewComponentVersionRepositoryRegistry(ctx)
	proto := &dummyv1.Repository{}
	require.NoError(t, RegisterInternalComponentVersionRepositoryPlugin(scheme, registry, &mockPlugin{}, proto))

	retrievedPlugin, err := GetReadWriteComponentVersionRepositoryPluginForType(ctx, registry, proto, scheme)
	require.NoError(t, err)
	desc, err := retrievedPlugin.GetComponentVersion(ctx, repov1.GetComponentVersionRequest[*dummyv1.Repository]{
		Repository: &dummyv1.Repository{
			Type: runtime.Type{
				Name:    "DummyRepository",
				Version: "v1",
			},
			BaseUrl: "base-url",
		},
		Name:    "test-mock-component",
		Version: "v1.0.0",
	}, map[string]string{})
	require.NoError(t, err)
	require.Equal(t, "test-mock-component:v1.0.0 (schema version 1.0.0)", desc.String())
}
