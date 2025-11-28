package componentlister

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/componentlister/v1"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var (
	dummyType = runtime.NewVersionedType(dummyv1.Type, dummyv1.Version)
)

func dummyCapability(schema []byte) v1.CapabilitySpec {
	return v1.CapabilitySpec{
		Type: runtime.NewUnversionedType(string(v1.ComponentListerPluginType)),
		SupportedRepositorySpecTypes: []mtypes.Type{{
			Type:       dummyType,
			JSONSchema: schema,
		}},
	}
}

func TestPluginFlow(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	path := filepath.Join("..", "..", "..", "tmp", "testdata", "test-plugin-component-lister")
	_, err := os.Stat(path)
	require.NoError(t, err, "test plugin not found, please build the plugin under tmp/testdata/test-plugin-component-lister first")

	ctx := context.Background()

	id := "test-plugin-component-lister" + time.Now().Format(time.RFC3339)

	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	registry := NewComponentListerRegistry(ctx)
	config := mtypes.Config{
		ID:         id,
		Type:       mtypes.Socket,
		PluginType: v1.ComponentListerPluginType,
	}
	serialized, err := json.Marshal(config)
	require.NoError(t, err)

	pluginCmd := exec.CommandContext(ctx, path, "--config", string(serialized))
	t.Cleanup(func() {
		_ = os.Remove(fmt.Sprintf("/tmp/%s-plugin.socket", id))
		_ = pluginCmd.Process.Kill()
	})
	pipe, err := pluginCmd.StdoutPipe()
	require.NoError(t, err)
	stderr, err := pluginCmd.StderrPipe()
	require.NoError(t, err)
	plugin := mtypes.Plugin{
		ID:     "test-plugin-component-lister",
		Path:   path,
		Config: config,
		Cmd:    pluginCmd,
		Stdout: pipe,
		Stderr: stderr,
	}

	capability := dummyCapability([]byte(`{}`))
	require.NoError(t, registry.AddPlugin(plugin, &capability))
	spec := &dummyv1.Repository{
		Type:    dummyType,
		BaseUrl: "example.com/test-repository",
	}
	require.NoError(t, err)
	retrievedListerPlugin, err := registry.GetComponentLister(ctx, spec, nil)
	require.NoError(t, err)

	expectedList := []string{"test-component-1", "test-component-2"}
	var result []string
	err = retrievedListerPlugin.ListComponents(ctx, "", func(names []string) error {
		result = append(result, names...)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, expectedList, result)

	// test error propagation.
	var resultIfErr []string
	err = retrievedListerPlugin.ListComponents(ctx, "last", func(names []string) error {
		resultIfErr = append(resultIfErr, names...)
		return nil
	})
	require.Error(t, err)
	require.Empty(t, resultIfErr)
	expectedErr := `unknown last: "last"`
	require.Truef(t, strings.Contains(err.Error(), expectedErr), "returned error '%s' does not contain expected '%s'", err.Error(), expectedErr)
}

func TestRegisterInternalComponentListerPlugin(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)

	registry := NewComponentListerRegistry(ctx)
	plugin := &mockInternalPlugin{}
	r.NoError(registry.RegisterInternalComponentListerPlugin(plugin))

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
			componentLister, err := registry.GetComponentLister(ctx, tc.repositorySpec, nil)
			tc.err(t, err)
			if err != nil {
				return
			}
			r.NotNil(componentLister)

			r.NoError(componentLister.ListComponents(ctx, "", nil))
			r.True(componentLister.(*mockInternalLister).called)
		})
	}
}

type mockInternalPlugin struct{}

var _ InternalComponentListerPluginContract = (*mockInternalPlugin)(nil)

func (m *mockInternalPlugin) GetComponentVersionRepositoryScheme() *runtime.Scheme {
	return dummytype.Scheme
}

func (m *mockInternalPlugin) GetComponentListerCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (identity runtime.Identity, err error) {
	panic("not implemented")
}

func (m *mockInternalPlugin) GetComponentLister(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (repository.ComponentLister, error) {
	return &mockInternalLister{}, nil
}

type mockInternalLister struct {
	called bool
}

var _ repository.ComponentLister = (*mockInternalLister)(nil)

func (m *mockInternalLister) ListComponents(ctx context.Context, last string, fn func(names []string) error) error {
	m.called = true
	return nil
}
