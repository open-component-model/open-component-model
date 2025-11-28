package blobtransformer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/blob/transformer"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/blobtransformer/v1"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var (
	dummyType = runtime.NewVersionedType(dummyv1.Type, dummyv1.Version)
)

func dummyCapability(schema []byte) v1.CapabilitySpec {
	return v1.CapabilitySpec{
		Type: runtime.NewUnversionedType(string(v1.BlobTransformerPluginType)),
		SupportedTransformerSpecTypes: []mtypes.Type{{
			Type:       dummyType,
			JSONSchema: schema,
		}},
	}
}

func TestPluginFlow(t *testing.T) {
	path := filepath.Join("..", "..", "..", "tmp", "testdata", "test-plugin-blobtransformer")
	_, err := os.Stat(path)
	require.NoError(t, err, "test plugin not found, please build the plugin under tmp/testdata/test-plugin-blobtransformer first")
	slog.SetLogLoggerLevel(slog.LevelDebug)

	ctx := t.Context()

	id := "test-plugin-blob-transformer" + time.Now().Format(time.RFC3339)

	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	registry := NewBlobTransformerRegistry(ctx)
	config := mtypes.Config{
		ID:         id,
		Type:       mtypes.Socket,
		PluginType: v1.BlobTransformerPluginType,
	}
	serialized, err := json.Marshal(config)
	require.NoError(t, err)

	pluginCmd := exec.CommandContext(ctx, path, "--config", string(serialized))
	t.Cleanup(func() {
		assert.NoError(t, pluginCmd.Process.Kill())
		err := os.Remove(fmt.Sprintf("/tmp/%s-plugin.socket", id))
		assert.True(t, err == nil || errors.Is(err, os.ErrNotExist))
	})
	pipe, err := pluginCmd.StdoutPipe()
	require.NoError(t, err)
	stderr, err := pluginCmd.StderrPipe()
	require.NoError(t, err)
	plugin := mtypes.Plugin{
		ID:     "test-plugin-blob-transformer",
		Path:   path,
		Config: config,
		Cmd:    pluginCmd,
		Stdout: pipe,
		Stderr: stderr,
	}
	capability := dummyCapability([]byte(`{}`))
	require.NoError(t, registry.AddPlugin(plugin, &capability))
	p, err := scheme.NewObject(dummyType)
	require.NoError(t, err)
	retrievedPlugin, err := registry.GetPlugin(ctx, p)
	require.NoError(t, err)

	transformedBlob, err := retrievedPlugin.TransformBlob(ctx, inmemory.New(strings.NewReader("foobar")), &dummyv1.Repository{
		Type:    dummyType,
		BaseUrl: "test-base-url",
	}, nil)
	require.NoError(t, err)
	require.NotNil(t, transformedBlob)
}

func TestPluginNotFound(t *testing.T) {
	ctx := context.Background()
	registry := NewBlobTransformerRegistry(ctx)
	proto := &dummyv1.Repository{
		Type: runtime.Type{
			Name:    "DummyRepository",
			Version: "v1",
		},
		BaseUrl: "",
	}
	_, err := registry.GetPlugin(ctx, proto)
	require.ErrorContains(t, err, "failed to get plugin for typ \"DummyRepository/v1\"")
}

func TestSchemeDoesNotExist(t *testing.T) {
	ctx := context.Background()
	registry := NewBlobTransformerRegistry(ctx)
	proto := &dummyv1.Repository{
		Type: runtime.Type{
			Name:    "DummyRepository",
			Version: "v1",
		},
		BaseUrl: "",
	}
	_, err := registry.GetPlugin(ctx, proto)
	require.ErrorContains(t, err, "failed to get plugin for typ \"DummyRepository/v1\"")
}

func TestInternalPluginRegistry(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)

	registry := NewBlobTransformerRegistry(ctx)
	mockPlugin := &mockBlobTransformerPlugin{}
	r.NoError(registry.RegisterInternalBlobTransformerPlugin(mockPlugin))

	tests := []struct {
		name              string
		transformerConfig runtime.Typed
		err               require.ErrorAssertionFunc
	}{
		{
			name:              "prototype",
			transformerConfig: &dummyv1.Repository{},
			err:               require.NoError,
		},
		{
			name: "canonical type",
			transformerConfig: &runtime.Raw{
				Type: runtime.Type{
					Name:    dummyv1.Type,
					Version: dummyv1.Version,
				},
			},
			err: require.NoError,
		},
		{
			name: "short type",
			transformerConfig: &runtime.Raw{
				Type: runtime.Type{
					Name:    dummyv1.ShortType,
					Version: dummyv1.Version,
				},
			},
			err: require.NoError,
		},
		{
			name: "invalid type",
			transformerConfig: &runtime.Raw{
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
			transformerPlugin, err := registry.GetPlugin(ctx, tc.transformerConfig)
			tc.err(t, err)
			if err != nil {
				return
			}
			r.NotNil(transformerPlugin)
			r.Equal(mockPlugin, transformerPlugin)

			transformedBlob, err := transformerPlugin.TransformBlob(ctx, inmemory.New(strings.NewReader("foobar")), nil, nil)
			require.NoError(t, err)
			require.True(t, mockPlugin.called)
			require.NotNil(t, transformedBlob)
		})
	}
}

func TestAddPluginDuplicate(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	registry := NewBlobTransformerRegistry(ctx)

	plugin := mtypes.Plugin{
		ID:   "test-plugin-duplicate",
		Path: "/path/to/plugin",
		Config: mtypes.Config{
			ID:         "test-plugin-duplicate",
			Type:       mtypes.Socket,
			PluginType: v1.BlobTransformerPluginType,
		},
	}

	// First registration should succeed
	capability := v1.CapabilitySpec{
		Type: runtime.NewUnversionedType(string(v1.BlobTransformerPluginType)),
		SupportedTransformerSpecTypes: []mtypes.Type{
			{
				Type:       dummyType,
				Aliases:    nil,
				JSONSchema: []byte(`{}`),
			},
		},
	}

	require.NoError(t, registry.AddPlugin(plugin, &capability))

	// Second registration should fail
	err := registry.AddPlugin(plugin, &capability)
	require.Error(t, err)
	require.Contains(t, err.Error(), "plugin with ID test-plugin-duplicate already registered")
}

func TestGetPluginWithEmptyType(t *testing.T) {
	ctx := context.Background()
	registry := NewBlobTransformerRegistry(ctx)

	proto := &dummyv1.Repository{
		Type: runtime.Type{}, // Empty type
	}

	_, err := registry.GetPlugin(ctx, proto)
	require.Error(t, err)
}

// Mock implementations for testing

type mockBlobTransformerPlugin struct {
	called bool
}

func (m *mockBlobTransformerPlugin) GetTransformerScheme() *runtime.Scheme {
	return dummytype.Scheme
}

func (m *mockBlobTransformerPlugin) GetBlobTransformerCredentialConsumerIdentity(ctx context.Context, spec runtime.Typed) (runtime.Identity, error) {
	m.called = true
	// Return a mock identity for testing purposes
	return runtime.Identity{}, nil
}

func (m *mockBlobTransformerPlugin) TransformBlob(ctx context.Context, blob blob.ReadOnlyBlob, spec runtime.Typed, credentials map[string]string) (blob.ReadOnlyBlob, error) {
	m.called = true
	return blob, nil
}

var _ transformer.Transformer = (*mockBlobTransformerPlugin)(nil)
