package digestprocessor

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/constructor"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/digestprocessor/v1"
	inputv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/input/v1"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var (
	dummyType = runtime.NewVersionedType(dummyv1.Type, dummyv1.Version)
)

func dummyCapability(schema []byte) v1.CapabilitySpec {
	return v1.CapabilitySpec{
		Type: runtime.NewUnversionedType(string(v1.DigestProcessorPluginType)),
		SupportedAccessTypes: []mtypes.Type{{
			Type:       dummyType,
			JSONSchema: schema,
		}},
	}
}

func TestPluginFlow(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	path := filepath.Join("..", "..", "..", "tmp", "testdata", "test-plugin-digester")
	_, err := os.Stat(path)
	require.NoError(t, err, "test plugin not found, please build the plugin under tmp/testdata/test-plugin-digester first")
	ctx := context.Background()
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	registry := NewDigestProcessorRegistry(ctx)
	config := mtypes.Config{
		ID:         "test-plugin-1-digester",
		Type:       mtypes.Socket,
		PluginType: inputv1.InputPluginType,
	}
	serialized, err := json.Marshal(config)
	require.NoError(t, err)

	pluginCmd := exec.CommandContext(ctx, path, "--config", string(serialized))
	pipe, err := pluginCmd.StdoutPipe()
	require.NoError(t, err)
	stderr, err := pluginCmd.StderrPipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove("/tmp/test-plugin-1-digester-plugin.socket")
		_ = pluginCmd.Process.Kill()
	})
	plugin := mtypes.Plugin{
		ID:     "test-plugin-1-construction",
		Path:   path,
		Stderr: stderr,
		Config: mtypes.Config{
			ID:         "test-plugin-1-construction",
			Type:       mtypes.Socket,
			PluginType: v1.DigestProcessorPluginType,
		},
		Cmd:    pluginCmd,
		Stdout: pipe,
	}

	capability := dummyCapability([]byte(`{}`))
	require.NoError(t, registry.AddPlugin(plugin, &capability))
	p, err := scheme.NewObject(dummyType)
	require.NoError(t, err)
	retrievedResourcePlugin, err := registry.GetPlugin(ctx, p)
	require.NoError(t, err)
	resource, err := retrievedResourcePlugin.ProcessResourceDigest(ctx, &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-resource-1",
				Version: "0.1.0",
			},
		},
		Type:     "type",
		Relation: "local",
		Access: &runtime.Raw{
			Type: dummyType,
			Data: []byte(`{ "access": "v1" }`),
		},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "test-resource", resource.Name)
}

func TestShutdown(t *testing.T) {
	// start up the plugin
	slog.SetLogLoggerLevel(slog.LevelDebug)
	path := filepath.Join("..", "..", "..", "tmp", "testdata", "test-plugin-digester")
	_, err := os.Stat(path)
	require.NoError(t, err, "test plugin not found, please build the plugin under tmp/testdata/test-plugin-digester first")
	ctx := context.Background()
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	registry := NewDigestProcessorRegistry(ctx)
	config := mtypes.Config{
		ID:         "test-plugin-1-digester",
		Type:       mtypes.Socket,
		PluginType: inputv1.InputPluginType,
	}
	serialized, err := json.Marshal(config)
	require.NoError(t, err)

	pluginCmd := exec.CommandContext(ctx, path, "--config", string(serialized))
	pipe, err := pluginCmd.StdoutPipe()
	require.NoError(t, err)
	stderr, err := pluginCmd.StderrPipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove("/tmp/test-plugin-1-digester-plugin.socket")
		_ = pluginCmd.Process.Kill()
	})
	plugin := mtypes.Plugin{
		ID:     "test-plugin-1-construction",
		Path:   path,
		Stderr: stderr,
		Config: mtypes.Config{
			ID:         "test-plugin-1-construction",
			Type:       mtypes.Socket,
			PluginType: v1.DigestProcessorPluginType,
		},
		Cmd:    pluginCmd,
		Stdout: pipe,
	}
	capability := v1.CapabilitySpec{
		Type: runtime.NewUnversionedType(string(v1.DigestProcessorPluginType)),
		SupportedAccessTypes: []mtypes.Type{
			{
				Type:       dummyType,
				Aliases:    nil,
				JSONSchema: []byte(`{}`),
			},
		},
	}

	require.NoError(t, registry.AddPlugin(plugin, &capability))
	retrievedPlugin, err := registry.GetPlugin(ctx, &runtime.Raw{Type: dummyType})
	require.NoError(t, err)
	require.NoError(t, registry.Shutdown(ctx))
	require.Eventually(t, func() bool {
		_, err = retrievedPlugin.ProcessResourceDigest(ctx, &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-resource-1",
					Version: "0.1.0",
				},
			},
			Type:     "type",
			Relation: "local",
			Access: &runtime.Raw{
				Type: runtime.Type{},
				Data: []byte("{}"),
			},
		}, nil)
		if err != nil {
			if strings.Contains(err.Error(), "failed to send request to plugin") {
				return true
			}

			t.Logf("error: %v", err)

			return false
		}

		return false
	}, 1*time.Second, 100*time.Millisecond)
}

func TestRegisterInternalDigestProcessorPlugin(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)

	registry := NewDigestProcessorRegistry(ctx)
	plugin := &mockDigestProcessorPlugin{}
	r.NoError(registry.RegisterInternalDigestProcessorPlugin(plugin))

	tests := []struct {
		name       string
		accessSpec runtime.Typed
		err        require.ErrorAssertionFunc
	}{
		{
			name:       "prototype",
			accessSpec: &dummyv1.Repository{},
			err:        require.NoError,
		},
		{
			name: "canonical type",
			accessSpec: &runtime.Raw{
				Type: runtime.Type{
					Name:    dummyv1.Type,
					Version: dummyv1.Version,
				},
			},
			err: require.NoError,
		},
		{
			name: "short type",
			accessSpec: &runtime.Raw{
				Type: runtime.Type{
					Name:    dummyv1.ShortType,
					Version: dummyv1.Version,
				},
			},
			err: require.NoError,
		},
		{
			name: "invalid type",
			accessSpec: &runtime.Raw{
				Type: runtime.Type{
					Name:    "NonExistingType",
					Version: "v1",
				},
			},
			err: require.Error,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name+"resource input", func(t *testing.T) {
			digesterProcessorPlugin, err := registry.GetPlugin(ctx, tc.accessSpec)
			tc.err(t, err)
			if err != nil {
				return
			}
			r.NotNil(digesterProcessorPlugin)
			r.Equal(plugin, digesterProcessorPlugin)

			_, err = digesterProcessorPlugin.ProcessResourceDigest(ctx, &descriptor.Resource{}, nil)
			require.NoError(t, err)
			require.True(t, plugin.called)
		})
	}
}

type mockDigestProcessorPlugin struct {
	called bool
}

func (m *mockDigestProcessorPlugin) GetResourceRepositoryScheme() *runtime.Scheme {
	return dummytype.Scheme
}

func (m *mockDigestProcessorPlugin) GetResourceDigestProcessorCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (identity runtime.Identity, err error) {
	return nil, nil
}

func (m *mockDigestProcessorPlugin) ProcessResourceDigest(ctx context.Context, resource *descriptor.Resource, credentials map[string]string) (*descriptor.Resource, error) {
	m.called = true

	return nil, nil
}

var _ constructor.ResourceDigestProcessor = (*mockDigestProcessorPlugin)(nil)
