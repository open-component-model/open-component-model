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
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestAddGetPlugin(t *testing.T) {
	stat, err := os.Stat(filepath.Join("testdata", "test-plugin"))
	require.NoError(t, err, "test plugin not found, please build the plugin under plugin/generic_plugin first")

	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	registry := NewComponentVersionRepositoryRegistry(scheme)
	ctx := context.Background()
	config := mtypes.Config{
		ID:         "test-plugin",
		Type:       mtypes.Socket,
		PluginType: mtypes.ComponentVersionRepositoryPluginType,
		Location:   "/tmp/test-plugin.socket",
	}
	serialized, err := json.Marshal(config)
	require.NoError(t, err)
	typ := runtime.Type{
		Version: "v1",
		Name:    "OCIRepository",
	}

	pluginCmd := exec.CommandContext(ctx, stat.Name(), "--config", string(serialized))
	plugin := &mtypes.Plugin{
		ID:   "test-plugin",
		Path: stat.Name(),
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
					JSONSchema: nil,
				},
			},
		},
		Cmd: pluginCmd,
	}
	require.NoError(t, registry.AddPlugin(plugin, typ))
}
