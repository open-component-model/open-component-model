package componentversionrepository

import (
	"context"
	"encoding/json"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestAddGetPlugin(t *testing.T) {
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

	pluginCmd := exec.CommandContext(ctx, testEnv.pluginLocation, "--config", string(serialized))
	plugin := &mtypes.Plugin{
		ID:   "test-plugin",
		Path: testEnv.pluginLocation,
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
