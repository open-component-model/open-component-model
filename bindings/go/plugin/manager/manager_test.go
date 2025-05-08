package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1"
	repov1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestPluginManager(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	ctx := t.Context()
	baseContext := context.Background()
	pm := NewPluginManager(baseContext)
	require.NoError(t, pm.RegisterPlugins(ctx, filepath.Join("..", "tmp", "testdata")))
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	proto := &v1.OCIRepository{}
	typ, err := scheme.TypeForPrototype(proto)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pm.Shutdown(ctx))
		require.NoError(t, os.Remove("/tmp/test-plugin-plugin.socket"))
	})

	plugin, err := componentversionrepository.GetReadWriteComponentVersionRepositoryPluginForType(ctx, pm.ComponentVersionRepositoryRegistry, proto, scheme)
	require.NoError(t, err)
	desc, err := plugin.GetComponentVersion(ctx, repov1.GetComponentVersionRequest[*v1.OCIRepository]{
		Repository: &v1.OCIRepository{
			Type:    typ,
			BaseUrl: "https://ocm.software/test",
		},
		Name:    "test-component",
		Version: "1.0.0",
	}, map[string]string{})
	require.NoError(t, err)
	require.Equal(t, "test-component:1.0.0", desc.String())
}

func TestPluginManagerCancelContext(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	ctx, cancel := context.WithCancel(context.Background())
	baseContext, baseCancel := context.WithCancel(context.Background()) // a different context
	pm := NewPluginManager(baseContext)
	require.NoError(t, pm.RegisterPlugins(ctx, filepath.Join("..", "tmp", "testdata")))
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	proto := &v1.OCIRepository{}
	t.Cleanup(func() {
		require.NoError(t, pm.Shutdown(ctx))
		require.NoError(t, os.Remove("/tmp/test-plugin-plugin.socket"))
	})

	// start the plugin
	plugin, err := componentversionrepository.GetReadWriteComponentVersionRepositoryPluginForType(ctx, pm.ComponentVersionRepositoryRegistry, proto, scheme)
	require.NoError(t, err)
	require.NoError(t, plugin.Ping(ctx))

	// cancelling the outer context should not shut down the plugin only the ongoing request.
	t.Log("cancelling outer context")
	cancel()
	require.NoError(t, plugin.Ping(context.Background()))
	t.Log("plugin is still alive, cancelling plugin context")
	baseCancel()
	require.Eventually(t, func() bool {
		err := plugin.Ping(context.Background())
		return err != nil
	}, 1*time.Second, 100*time.Millisecond)
	t.Log("plugin is stopped")
}

func TestPluginManagerShutdownPlugin(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	ctx := context.Background()
	baseContext := context.Background() // a different context
	pm := NewPluginManager(baseContext)
	require.NoError(t, pm.RegisterPlugins(ctx, filepath.Join("..", "tmp", "testdata")))
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	proto := &v1.OCIRepository{}
	t.Cleanup(func() {
		// make sure it's gone even if the test fails, but ignore the deletion error since it should be removed.
		_ = os.Remove("/tmp/test-plugin-plugin.socket")
	})

	// start the plugin
	plugin, err := componentversionrepository.GetReadWriteComponentVersionRepositoryPluginForType(ctx, pm.ComponentVersionRepositoryRegistry, proto, scheme)
	require.NoError(t, err)
	require.NoError(t, plugin.Ping(ctx))

	// cancelling the outer context should not shut down the plugin only the ongoing request.
	require.NoError(t, pm.Shutdown(ctx))
	require.Eventually(t, func() bool {
		err := plugin.Ping(ctx)
		return err != nil
	}, 1*time.Second, 100*time.Millisecond)
	_, err = os.Stat("/tmp/test-plugin-plugin.socket")
	require.Error(t, err)
}

func TestPluginManagerShutdownWithoutWait(t *testing.T) {
	writer := bytes.NewBuffer(nil)
	slog.SetDefault(slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))
	ctx, cancel := context.WithCancel(context.Background())
	baseContext := context.Background() // a different context
	pm := NewPluginManager(baseContext)
	require.NoError(t, pm.RegisterPlugins(ctx, filepath.Join("..", "tmp", "testdata")))
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	proto := &v1.OCIRepository{}
	t.Cleanup(func() {
		// make sure it's gone even if the test fails, but ignore the deletion error since it should be removed.
		_ = os.Remove("/tmp/test-plugin-plugin.socket")
	})

	// start the plugin
	plugin, err := componentversionrepository.GetReadWriteComponentVersionRepositoryPluginForType(ctx, pm.ComponentVersionRepositoryRegistry, proto, scheme)
	require.NoError(t, err)
	require.NoError(t, plugin.Ping(ctx))

	// Cancelling the shutdown context still should allow a graceful shutdown of the plugin because
	// that context is outside of this. This shutdown will just send an interrupt signal to the
	// underlying command.
	require.NoError(t, pm.Shutdown(ctx))
	cancel()

	require.Eventually(t, func() bool {
		err := plugin.Ping(ctx)
		return err != nil
	}, 1*time.Second, 100*time.Millisecond)

	content, err := io.ReadAll(writer)
	require.NoError(t, err)
	require.Contains(t, string(content), "Gracefully shutting down plugin id=test-plugin")
}

func TestPluginManagerMultiplePluginsForSameType(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	ctx := t.Context()
	baseContext := context.Background()
	pm := NewPluginManager(baseContext)
	pluginTypes := types.Types{
		Types: map[types.PluginType][]types.Type{
			types.ComponentVersionRepositoryPluginType: {
				{
					Type: runtime.Type{
						Name:    "OCIRepository",
						Version: "v1",
					},
				},
			},
		},
	}
	testPlugin := types.Plugin{
		ID:   "test-id",
		Path: "/tmp/test-plugin-plugin.socket",
		Config: types.Config{
			ID:         "test-id",
			Type:       "unix",
			PluginType: types.ComponentVersionRepositoryPluginType,
		},
	}
	serialized, err := json.Marshal(pluginTypes)
	require.NoError(t, err)

	require.NoError(t, pm.addPlugin(ctx, testPlugin, bytes.NewBuffer(serialized)))
	// trying to add the same plugin again for the same type but with different id
	// this way of testing actually showed a horrible flaw. We were passing around a pointer
	// which meant if we weren't very careful and overwrote the plugin AFTER we added it,
	// the plugin inside the map of the registry was also changed. That is really, really not desired.
	testPlugin.ID = "test-other"
	testPlugin.Path = "/tmp/test-other-plugin-plugin.socket"
	testPlugin.Config.ID = "test-other"
	testPlugin.Config.Type = "tcp"
	testPlugin.Types = nil
	require.ErrorContains(t, pm.addPlugin(ctx, testPlugin, bytes.NewBuffer(serialized)), "failed to register plugin test-other: plugin for type OCIRepository/v1 already registered with ID: test-id")
}

func TestPluginManagerWithNoPlugins(t *testing.T) {
	pm := NewPluginManager(context.Background())
	require.ErrorContains(t, pm.RegisterPlugins(context.Background(), filepath.Join(".")), "no plugins found")
}
