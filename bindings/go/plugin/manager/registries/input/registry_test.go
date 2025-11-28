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
	constructor2 "ocm.software/open-component-model/bindings/go/constructor"
	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	inputv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/input/v1"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var (
	dummyType = runtime.NewVersionedType(dummyv1.Type, dummyv1.Version)
)

func dummyCapability(schema []byte) inputv1.CapabilitySpec {
	return inputv1.CapabilitySpec{
		Type: runtime.NewUnversionedType(string(inputv1.InputPluginType)),
		SupportedInputTypes: []mtypes.Type{{
			Type:       dummyType,
			JSONSchema: schema,
		}},
	}
}

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
			PluginType: inputv1.InputPluginType,
		},
		Cmd:    pluginCmd,
		Stdout: pipe,
	}
	capability := dummyCapability([]byte(`{}`))
	require.NoError(t, registry.AddPlugin(plugin, &capability))
	retrievedResourcePlugin, err := registry.GetResourceInputPlugin(ctx, &runtime.Raw{Type: dummyType})
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
			Input: &runtime.Raw{
				Type: dummyType,
				Data: []byte(`{ "access": "v1" }`),
			},
		},
	}, map[string]string{})
	require.NoError(t, err)
	require.Equal(t, "test-resource", resource.ProcessedResource.Name)

	retrievedSourcePlugin, err := registry.GetSourceInputPlugin(ctx, &runtime.Raw{Type: dummyType})
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
			Input: &runtime.Raw{
				Type: dummyType,
				Data: []byte(`{ "access": "v1" }`),
			},
		},
	}, map[string]string{})
	require.NoError(t, err)
	require.Equal(t, "test-source", source.ProcessedSource.Name)
}

func TestRegisterInternalResourceInputPlugin(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)

	registry := NewInputRepositoryRegistry(ctx)
	resourcePlugin := &mockResourceInputPlugin{}
	sourcePlugin := &mockSourceInputPlugin{}
	r.NoError(registry.RegisterInternalResourceInputPlugin(resourcePlugin))
	r.NoError(registry.RegisterInternalSourceInputPlugin(sourcePlugin))

	tests := []struct {
		name      string
		inputSpec runtime.Typed
		err       require.ErrorAssertionFunc
	}{
		{
			name:      "prototype",
			inputSpec: &dummyv1.Repository{},
			err:       require.NoError,
		},
		{
			name: "canonical type",
			inputSpec: &runtime.Raw{
				Type: runtime.Type{
					Name:    dummyv1.Type,
					Version: dummyv1.Version,
				},
			},
			err: require.NoError,
		},
		{
			name: "short type",
			inputSpec: &runtime.Raw{
				Type: runtime.Type{
					Name:    dummyv1.ShortType,
					Version: dummyv1.Version,
				},
			},
			err: require.NoError,
		},
		{
			name: "invalid type",
			inputSpec: &runtime.Raw{
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
			resourceInputMethod, err := registry.GetResourceInputPlugin(ctx, tc.inputSpec)
			tc.err(t, err)
			if err != nil {
				return
			}
			r.NotNil(resourceInputMethod)
			r.Equal(resourcePlugin, resourceInputMethod)

			_, err = resourceInputMethod.ProcessResource(ctx, &constructor.Resource{}, nil)
			require.NoError(t, err)
			require.True(t, resourcePlugin.processCalled)
		})
		t.Run(tc.name+"source input", func(t *testing.T) {
			sourceInputMethod, err := registry.GetSourceInputPlugin(ctx, tc.inputSpec)
			tc.err(t, err)
			if err != nil {
				return
			}
			r.NotNil(sourceInputMethod)
			r.Equal(sourcePlugin, sourceInputMethod)

			_, err = sourceInputMethod.ProcessSource(ctx, &constructor.Source{}, nil)
			require.NoError(t, err)
			require.True(t, sourcePlugin.processCalled)
		})
	}
}

type mockResourceInputPlugin struct {
	credCalled    bool
	processCalled bool
}

func (m *mockResourceInputPlugin) GetInputMethodScheme() *runtime.Scheme {
	return dummytype.Scheme
}

func (m *mockResourceInputPlugin) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *constructor.Resource) (identity runtime.Identity, err error) {
	m.credCalled = true
	return nil, nil
}

func (m *mockResourceInputPlugin) ProcessResource(ctx context.Context, resource *constructor.Resource, credentials map[string]string) (result *constructor2.ResourceInputMethodResult, err error) {
	m.processCalled = true
	return nil, nil
}

var _ constructor2.ResourceInputMethod = (*mockResourceInputPlugin)(nil)

type mockSourceInputPlugin struct {
	credCalled    bool
	processCalled bool
}

func (m *mockSourceInputPlugin) GetInputMethodScheme() *runtime.Scheme {
	return dummytype.Scheme
}

func (m *mockSourceInputPlugin) GetSourceCredentialConsumerIdentity(ctx context.Context, source *constructor.Source) (identity runtime.Identity, err error) {
	m.credCalled = true
	return nil, nil
}

func (m *mockSourceInputPlugin) ProcessSource(ctx context.Context, resource *constructor.Source, credentials map[string]string) (result *constructor2.SourceInputMethodResult, err error) {
	m.processCalled = true
	return nil, nil
}

var _ constructor2.SourceInputMethod = (*mockSourceInputPlugin)(nil)
