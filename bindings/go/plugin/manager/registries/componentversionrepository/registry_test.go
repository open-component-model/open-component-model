package componentversionrepository

import (
	"context"
	"encoding/json"
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
	path := filepath.Join("..", "..", "..", "tmp", "testdata", "test-plugin-component-version")
	_, err := os.Stat(path)
	require.NoError(t, err, "test plugin not found, please build the plugin under tmp/testdata/test-plugin-component-version first")
	slog.SetLogLoggerLevel(slog.LevelDebug)

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
	stderr, err := pluginCmd.StderrPipe()
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
		Stderr: stderr,
	}
	require.NoError(t, registry.AddPlugin(plugin, typ))
	spec := &dummyv1.Repository{
		Type:    typ,
		BaseUrl: "ghcr.io/open-component/test-component-version-repository",
	}
	retrievedPlugin, err := registry.GetComponentVersionRepository(ctx, spec, nil)
	require.NoError(t, err)
	desc, err := retrievedPlugin.GetComponentVersion(ctx, "test-component", "1.0.0")
	require.NoError(t, err)
	require.Equal(t, "test-component:1.0.0", desc.String())

	err = retrievedPlugin.AddComponentVersion(ctx, &descriptor.Descriptor{
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-component",
					Version: "1.0.0",
				},
			},
			Provider: descriptor.Provider{
				Name: "ocm.software",
			},
		}})
	require.NoError(t, err)
}

func TestPluginNotFound(t *testing.T) {
	ctx := context.Background()
	registry := NewComponentVersionRepositoryRegistry(ctx)
	proto := &dummyv1.Repository{
		Type: runtime.Type{
			Name:    "DummyRepository",
			Version: "v1",
		},
		BaseUrl: "",
	}
	_, err := registry.GetComponentVersionRepository(ctx, proto, nil)
	require.ErrorContains(t, err, "failed to get plugin for typ \"DummyRepository/v1\"")
}

func TestSchemeDoesNotExist(t *testing.T) {
	ctx := context.Background()
	registry := NewComponentVersionRepositoryRegistry(ctx)
	proto := &dummyv1.Repository{
		Type: runtime.Type{
			Name:    "DummyRepository",
			Version: "v1",
		},
		BaseUrl: "",
	}
	_, err := registry.GetComponentVersionRepository(ctx, proto, nil)
	require.ErrorContains(t, err, "failed to get plugin for typ \"DummyRepository/v1\"")
}

type mockRepository struct{}

func (m *mockRepository) AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error {
	return nil
}

func (m *mockRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	return nil, nil
}

func (m *mockRepository) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	return nil, nil
}

func (m *mockRepository) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	return nil, nil
}

func (m *mockRepository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	return nil, nil, nil
}

func (m *mockRepository) AddLocalSource(ctx context.Context, component, version string, src *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	return nil, nil
}

func (m *mockRepository) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	return nil, nil, nil
}

func TestInternalPluginRegistry(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	registry := NewComponentVersionRepositoryRegistry(ctx)
	proto := &dummyv1.Repository{
		Type: runtime.Type{
			Name:    "DummyRepository",
			Version: "v1",
		},
		BaseUrl: "",
	}
	require.NoError(t, RegisterInternalComponentVersionRepositoryPlugin(scheme, registry, &mockRepository{}, proto))
	retrievedPlugin, err := registry.GetComponentVersionRepository(ctx, proto, nil)
	require.NoError(t, err)
	// The retrieved plugin is now directly a ComponentVersionRepository
	require.NotNil(t, retrievedPlugin)
}
