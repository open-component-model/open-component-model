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

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var (
	dummyType = runtime.NewVersionedType(dummyv1.Type, dummyv1.Version)
)

func dummyCapability(schema []byte) v1.CapabilitySpec {
	return v1.CapabilitySpec{
		Type: runtime.NewUnversionedType(string(v1.ComponentVersionRepositoryPluginType)),
		SupportedRepositorySpecTypes: []mtypes.Type{{
			Type:       dummyType,
			JSONSchema: schema,
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
			PluginType: v1.ComponentVersionRepositoryPluginType,
		},
		Cmd:    pluginCmd,
		Stdout: pipe,
		Stderr: stderr,
	}
	capability := dummyCapability([]byte(`{}`))
	require.NoError(t, registry.AddPlugin(plugin, &capability))
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
	ctx := t.Context()
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

func TestInternalPluginRegistry(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)

	registry := NewComponentVersionRepositoryRegistry(ctx)
	repositoryProvider := &mockPluginProvider{
		mockPlugin: &mockedRepository{},
	}
	r.NoError(registry.RegisterInternalComponentVersionRepositoryPlugin(repositoryProvider))

	tests := []struct {
		name           string
		repositorySpec runtime.Typed
		err            require.ErrorAssertionFunc
	}{
		{
			name:           "prototype",
			repositorySpec: &dummyv1.Repository{},
			err:            require.NoError,
		},
		{
			name: "canonical type",
			repositorySpec: &runtime.Raw{
				Type: runtime.Type{
					Name:    dummyv1.Type,
					Version: dummyv1.Version,
				},
			},
			err: require.NoError,
		},
		{
			name: "short type",
			repositorySpec: &runtime.Raw{
				Type: runtime.Type{
					Name:    dummyv1.ShortType,
					Version: dummyv1.Version,
				},
			},
			err: require.NoError,
		},
		{
			name: "invalid type",
			repositorySpec: &runtime.Raw{
				Type: runtime.Type{
					Name:    "NonExistingType",
					Version: "v1",
				},
			},
			err: require.Error,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			identity, err := registry.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, tc.repositorySpec)
			tc.err(t, err)
			if err != nil {
				return
			}
			expectedIdentity := runtime.Identity{
				"test": "identity",
			}
			r.Equal(expectedIdentity, identity)
		})

		t.Run(tc.name, func(t *testing.T) {
			retrievedPluginProvider, err := registry.GetComponentVersionRepository(ctx, tc.repositorySpec, nil)
			tc.err(t, err)
			if err != nil {
				return
			}
			r.NotNil(retrievedPluginProvider)
		})
	}
}

type mockPluginProvider struct {
	mockPlugin repository.ComponentVersionRepository
}

func (m *mockPluginProvider) GetComponentVersionRepositoryScheme() *runtime.Scheme {
	return dummytype.Scheme
}

func (m *mockPluginProvider) GetJSONSchemaForRepositorySpecification(typ runtime.Type) ([]byte, error) {
	return dummyv1.Repository{}.JSONSchema(), nil
}

var _ repository.ComponentVersionRepositoryProvider = (*mockPluginProvider)(nil)

func (m *mockPluginProvider) GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	return runtime.Identity{
		"test": "identity",
	}, nil
}

func (m *mockPluginProvider) GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (repository.ComponentVersionRepository, error) {
	return m.mockPlugin, nil
}

type mockedRepository struct {
	dummyv1.Repository
	// No need to provide a mock implementation for the repository here, we
	// test the provider.
	repository.ComponentVersionRepository
}

var (
	_ runtime.Typed                         = (*mockedRepository)(nil)
	_ repository.ComponentVersionRepository = (*mockedRepository)(nil)
)
