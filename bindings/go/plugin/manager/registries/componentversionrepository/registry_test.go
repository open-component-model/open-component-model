package componentversionrepository

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestPluginFlow(t *testing.T) {
	path := filepath.Join("testdata", "test-plugin")
	_, err := os.Stat(path)
	require.NoError(t, err, "test plugin not found, please build the plugin under plugin/generic_plugin first")

	ctx := context.Background()
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	registry := NewComponentVersionRepositoryRegistry(ctx)
	config := mtypes.Config{
		ID:         "test-plugin",
		Type:       mtypes.Socket,
		PluginType: mtypes.ComponentVersionRepositoryPluginType,
		Location:   "/tmp/test-plugin.socket",
	}
	serialized, err := json.Marshal(config)
	require.NoError(t, err)

	proto := &v1.OCIRepository{}
	typ, err := scheme.TypeForPrototype(proto)
	require.NoError(t, err)

	pluginCmd := exec.CommandContext(ctx, path, "--config", string(serialized))
	t.Cleanup(func() {
		_ = pluginCmd.Process.Kill()
		_ = os.Remove("/tmp/test-plugin.socket")
	})
	plugin := &mtypes.Plugin{
		ID:   "test-plugin",
		Path: path,
		Config: mtypes.Config{
			ID:         "test-plugin",
			Type:       mtypes.Socket,
			PluginType: mtypes.ComponentVersionRepositoryPluginType,
			Location:   "/tmp/test-plugin.socket",
		},
		Types: map[mtypes.PluginType][]mtypes.Type{
			mtypes.ComponentVersionRepositoryPluginType: {
				{
					Type:       typ,
					JSONSchema: []byte(`{}`),
				},
			},
		},
		Cmd: pluginCmd,
	}
	require.NoError(t, registry.AddPlugin(plugin, typ))

	retrievedPlugin, err := GetReadWriteComponentVersionRepositoryPluginForType(ctx, registry, proto, scheme)
	require.NoError(t, err)
	desc, err := retrievedPlugin.GetComponentVersion(ctx, mtypes.GetComponentVersionRequest[*v1.OCIRepository]{
		Repository: &v1.OCIRepository{
			Type: runtime.Type{
				Name:    "OCIRepository",
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
