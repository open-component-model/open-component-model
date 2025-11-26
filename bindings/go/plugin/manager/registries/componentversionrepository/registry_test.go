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
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	"ocm.software/open-component-model/bindings/go/runtime"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/repository"
)

var (
	DummyType = runtime.NewVersionedType(dummyv1.Type, dummyv1.Version)
)

func DummyCapability(schema []byte) v1.CapabilitySpec {
	return v1.CapabilitySpec{
		Type: runtime.NewUnversionedType(string(v1.ComponentVersionRepositoryPluginType)),
		TypeToJSONSchema: map[string][]byte{
			DummyType.String(): schema,
		},
		SupportedRepositorySpecTypes: []mtypes.Type{{
			Type: DummyType,
		}},
	}
}

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
		PluginType: v1.ComponentVersionRepositoryPluginType,
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
		Cmd:    pluginCmd,
		Stdout: pipe,
		Stderr: stderr,
	}
	capability := DummyCapability([]byte(`{}`))
	require.NoError(t, registry.AddPluginWithAliases(plugin, &capability))
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

type mockPluginProvider struct {
	mockPlugin repository.ComponentVersionRepository
}

func (m *mockPluginProvider) GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	return runtime.Identity{
		"test": "identity",
	}, nil
}

func (m *mockPluginProvider) GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (repository.ComponentVersionRepository, error) {
	return m.mockPlugin, nil
}

type mockedRepository struct {
	repository.ComponentVersionRepository
}

var _ repository.ComponentVersionRepository = &mockedRepository{}

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
	require.NoError(t, RegisterInternalComponentVersionRepositoryPlugin(scheme, registry, &mockPluginProvider{
		mockPlugin: &mockedRepository{},
	}, proto))

	identity, err := registry.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, proto)
	require.NoError(t, err)
	require.NotNil(t, identity)

	retrievedPluginProvider, err := registry.GetComponentVersionRepository(ctx, proto, nil)
	require.NoError(t, err)
	require.NotNil(t, retrievedPluginProvider)
}
