package resource

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/blob"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"

	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
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
	resource, err := retrievedPlugin.DownloadResource(ctx, &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
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
	}, map[string]string{})
	require.NoError(t, err)
	reader, err := resource.ReadCloser()
	require.NoError(t, err)
	defer reader.Close()
	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, "test-resource", string(content))
	//require.Equal(t, "/tmp/to/file", resource.Value)

	// Test adding a resource
	//addedResource, err := retrievedPlugin.AddGlobalResource(ctx, &v1.AddGlobalResourceRequest{
	//	Resource: &descriptorv2.Resource{
	//		ElementMeta: descriptorv2.ElementMeta{
	//			ObjectMeta: descriptorv2.ObjectMeta{
	//				Name:    "test-resource-2",
	//				Version: "0.1.0",
	//			},
	//		},
	//		Type:     "type",
	//		Relation: "local",
	//		Access: &runtime.Raw{
	//			Type: runtime.Type{
	//				Version: "test-access",
	//				Name:    "v1",
	//			},
	//			Data: []byte(`{ "access": "v1" }`),
	//		},
	//	},
	//}, map[string]string{})
	//require.NoError(t, err)
	//require.Equal(t, "test-global-resource", addedResource.Resource.Name)
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

func (m *mockResourcePlugin) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *constructorruntime.Resource) (identity runtime.Identity, err error) {
	return nil, nil
}

func (m *mockResourcePlugin) DownloadResource(ctx context.Context, res *descriptor.Resource, credentials map[string]string) (content blob.ReadOnlyBlob, err error) {
	return blob.NewDirectReadOnlyBlob(bytes.NewBufferString("test-resource")), nil
}
