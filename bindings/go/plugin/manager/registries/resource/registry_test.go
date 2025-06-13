package resource

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/resource/v1"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestPluginFlow(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	path := filepath.Join("..", "..", "..", "tmp", "testdata", "test-plugin-resource")
	_, err := os.Stat(path)
	require.NoError(t, err, "test plugin not found, please build the plugin under tmp/testdata/test-plugin-resource first")
	ctx := context.Background()
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	registry := NewResourceRegistry(ctx)
	config := mtypes.Config{
		ID:         "test-plugin-1-resource",
		Type:       mtypes.Socket,
		PluginType: mtypes.ResourceRepositoryPluginType,
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
		_ = os.Remove("/tmp/test-plugin-1-resource-plugin.socket")
		_ = pluginCmd.Process.Kill()
	})
	plugin := mtypes.Plugin{
		ID:     "test-plugin-1-resource",
		Path:   path,
		Stderr: stderr,
		Config: mtypes.Config{
			ID:         "test-plugin-1-resource",
			Type:       mtypes.Socket,
			PluginType: mtypes.ResourceRepositoryPluginType,
		},
		Types: map[mtypes.PluginType][]mtypes.Type{
			mtypes.ResourceRepositoryPluginType: {
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
	retrievedPlugin, err := registry.GetResourcePlugin(ctx, p)
	require.NoError(t, err)
	require.NoError(t, retrievedPlugin.Ping(ctx))
	resource, err := retrievedPlugin.GetGlobalResource(ctx, &v1.GetGlobalResourceRequest{
		Resource: &descriptorv2.Resource{
			ElementMeta: descriptorv2.ElementMeta{
				ObjectMeta: descriptorv2.ObjectMeta{
					Name:    "test-resource-1",
					Version: "0.1.0",
				},
			},
			Type:     "type",
			Relation: "local",
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
	require.Equal(t, "/tmp/to/file", resource.Location.Value)

	// Test adding a resource
	addedResource, err := retrievedPlugin.AddGlobalResource(ctx, &v1.AddGlobalResourceRequest{
		Resource: &descriptorv2.Resource{
			ElementMeta: descriptorv2.ElementMeta{
				ObjectMeta: descriptorv2.ObjectMeta{
					Name:    "test-resource-2",
					Version: "0.1.0",
				},
			},
			Type:     "type",
			Relation: "local",
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
	require.Equal(t, "test-global-resource", addedResource.Resource.Name)
}

func TestRegisterInternalResourcePlugin(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	registry := NewResourceRegistry(ctx)

	// Create a mock plugin
	mockPlugin := &mockResourcePlugin{}

	// Create a prototype
	proto := &dummyv1.Repository{}
	scheme.MustRegister(proto, "v1")

	// Register the internal plugin
	err := RegisterInternalResourcePlugin(scheme, registry, mockPlugin, proto)
	require.NoError(t, err)

	// Verify the plugin was registered
	_, err = scheme.TypeForPrototype(proto)
	require.NoError(t, err)

	// Get the plugin and verify it's the same instance
	plugin, err := registry.GetResourcePlugin(ctx, proto)
	require.NoError(t, err)
	require.Equal(t, mockPlugin, plugin)
}

type mockResourcePlugin struct{}

func (m *mockResourcePlugin) GetIdentity(ctx context.Context, request *v1.GetIdentityRequest[runtime.Typed]) (*v1.GetIdentityResponse, error) {
	return nil, nil
}

func (m *mockResourcePlugin) GetGlobalResource(ctx context.Context, request *v1.GetGlobalResourceRequest, creds map[string]string) (*v1.GetGlobalResourceResponse, error) {
	return nil, nil
}

func (m *mockResourcePlugin) AddGlobalResource(ctx context.Context, request *v1.AddGlobalResourceRequest, creds map[string]string) (*v1.AddGlobalResourceResponse, error) {
	return nil, nil
}

func (m *mockResourcePlugin) Ping(ctx context.Context) error {
	return nil
}
