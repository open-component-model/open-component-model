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
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

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
		PluginType: mtypes.ComponentListerPluginType,
	}
	serialized, err := json.Marshal(config)
	require.NoError(t, err)

	proto := &dummyv1.Repository{}
	typ, err := scheme.TypeForPrototype(proto)
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
		Types: map[mtypes.PluginType][]mtypes.Type{
			mtypes.ComponentListerPluginType: {
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
		BaseUrl: "example.com/test-repository",
	}
	require.NoError(t, err)
	retrievedListerPlugin, err := registry.GetComponentListerPlugin(ctx, spec, nil)
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
	ctx := context.Background()
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	registry := NewComponentListerRegistry(ctx)
	p := &mockComponentListerPlugin{}
	require.NoError(t, RegisterInternalComponentListerPlugin(scheme, registry, p, &dummyv1.Repository{}))
	retrievedPlugin, err := registry.GetComponentListerPlugin(ctx, &dummyv1.Repository{}, nil)
	require.NoError(t, err)
	require.Equal(t, p, retrievedPlugin)
	result := []string{}
	err = retrievedPlugin.ListComponents(ctx, "", func(names []string) error {
		result = append(result, names...)
		return nil
	})
	require.NoError(t, err)
	require.True(t, p.processCalled)
}

type mockComponentListerPlugin struct {
	credCalled    bool
	processCalled bool
}

func (m *mockComponentListerPlugin) GetComponentListerCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (identity runtime.Identity, err error) {
	m.credCalled = true
	return nil, nil
}

func (m *mockComponentListerPlugin) ListComponents(ctx context.Context, last string, fn func(names []string) error) error {
	m.processCalled = true
	return nil
}

var _ InternalComponentListerPluginContract = (*mockComponentListerPlugin)(nil)
