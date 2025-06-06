package input

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"

	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestPluginFlow(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	path := filepath.Join("..", "..", "..", "tmp", "testdata", "test-plugin-input")
	_, err := os.Stat(path)
	require.NoError(t, err, "test plugin not found, please build the plugin under tmp/testdata/test-plugin-input first")
	ctx := context.Background()
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	registry := NewInputRepositoryRegistry(ctx)
	config := mtypes.Config{
		ID:         "test-plugin-1-construction",
		Type:       mtypes.Socket,
		PluginType: mtypes.InputPluginType,
	}
	serialized, err := json.Marshal(config)
	require.NoError(t, err)

	proto := &dummyv1.Repository{}
	typ, err := scheme.TypeForPrototype(proto)
	require.NoError(t, err)

	pluginCmd := exec.CommandContext(ctx, path, "--config", string(serialized))
	pipe, err := pluginCmd.StdoutPipe()
	require.NoError(t, err)
	stderr, err := pluginCmd.StderrPipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove("/tmp/test-plugin-1-construction-plugin.socket")
		_ = pluginCmd.Process.Kill()
	})
	plugin := mtypes.Plugin{
		ID:     "test-plugin-1-construction",
		Path:   path,
		Stderr: stderr,
		Config: mtypes.Config{
			ID:         "test-plugin-1-construction",
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
	p, err := scheme.NewObject(typ)
	require.NoError(t, err)
	retrievedResourcePlugin, err := registry.GetResourceInputPlugin(ctx, p)
	require.NoError(t, err)
	resource, err := retrievedResourcePlugin.ProcessResource(ctx, &constructor.Resource{
		ElementMeta: constructor.ElementMeta{
			ObjectMeta: constructor.ObjectMeta{
				Name:    "test-resource-1",
				Version: "0.1.0",
			},
		},
		Type:     "type",
		Relation: "local",
		AccessOrInput: constructor.AccessOrInput{
			Access: &runtime.Raw{
				Type: runtime.Type{
					Version: "test-access",
					Name:    "v1",
				},
				Data: []byte(`{ "access": "v1" }`),
			},
		},
	}, map[string]string{})
	require.NoError(t, err)
	require.Equal(t, "test-resource", resource.ProcessedResource.Name)

	retrievedSourcePlugin, err := registry.GetSourceInputPlugin(ctx, p)
	require.NoError(t, err)
	source, err := retrievedSourcePlugin.ProcessSource(ctx, &constructor.Source{
		ElementMeta: constructor.ElementMeta{
			ObjectMeta: constructor.ObjectMeta{
				Name:    "test-source-1",
				Version: "0.1.0",
			},
		},
		Type: "type",
		AccessOrInput: constructor.AccessOrInput{
			Access: &runtime.Raw{
				Type: runtime.Type{
					Version: "test-access",
					Name:    "v1",
				},
				Data: []byte(`{ "access": "v1" }`),
			},
		},
	}, map[string]string{})
	require.NoError(t, err)
	require.Equal(t, "test-source", source.ProcessedSource.Name)
}
