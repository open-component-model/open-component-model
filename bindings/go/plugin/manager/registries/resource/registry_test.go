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
}

func TestRegisterInternalResourcePlugin(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	registry := NewResourceRegistry(ctx)

	mockPlugin := &mockResourcePlugin{}
	proto := &dummyv1.Repository{}
	scheme.MustRegister(proto, "v1")
	err := RegisterInternalResourcePlugin(scheme, registry, mockPlugin, proto)
	require.NoError(t, err)
	_, err = scheme.TypeForPrototype(proto)
	require.NoError(t, err)

	plugin, err := registry.GetResourcePlugin(ctx, proto)
	require.NoError(t, err)
	require.Equal(t, mockPlugin, plugin)
}

type mockResourcePlugin struct{}

var _ Repository = (*mockResourcePlugin)(nil)

func (m *mockResourcePlugin) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	return nil, nil
}

func (m *mockResourcePlugin) DownloadResource(ctx context.Context, res *descriptor.Resource, credentials map[string]string) (blob.ReadOnlyBlob, error) {
	return blob.NewDirectReadOnlyBlob(bytes.NewBufferString("test-resource")), nil
}
