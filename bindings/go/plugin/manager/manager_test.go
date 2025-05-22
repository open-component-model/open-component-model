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

	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	repov1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
	v1 "ocm.software/open-component-model/cli/configuration/v1"
)

func TestPluginManager(t *testing.T) {
	config := &v1.Config{
		Type: runtime.Type{
			Name:    "custom.config",
			Version: "v1",
		},
		Configurations: []*runtime.Raw{
			{
				Type: runtime.Type{
					Name:    "custom.config",
					Version: "v1",
				},
				Data: []byte(`{}`),
			},
		},
	}
	slog.SetLogLoggerLevel(slog.LevelDebug)
	ctx := t.Context()
	baseContext := context.Background()
	pm := NewPluginManager(baseContext)
	require.NoError(t, pm.RegisterPlugins(ctx, config, filepath.Join("..", "tmp", "testdata")))
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	typ, err := scheme.TypeForPrototype(&dummyv1.Repository{})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pm.Shutdown(ctx))
		// make sure it's not there but during a proper shutdown now this is removed by the plugin
		_ = os.Remove("/tmp/test-plugin-plugin.socket")
	})

	proto, err := scheme.NewObject(typ)
	require.NoError(t, err)
	plugin, err := pm.ComponentVersionRepositoryRegistry.GetPlugin(ctx, proto)
	require.NoError(t, err)
	desc, err := plugin.GetComponentVersion(ctx, repov1.GetComponentVersionRequest[runtime.Typed]{
		Repository: &dummyv1.Repository{
			Type:    typ,
			BaseUrl: "https://ocm.software/test",
		},
		Name:    "test-component",
		Version: "1.0.0",
	}, map[string]string{})
	require.NoError(t, err)
	require.Equal(t, "test-component:1.0.0", desc.String())
}

func TestConfigurationPassedToPlugin(t *testing.T) {
	config := &v1.Config{
		Type: runtime.Type{
			Version: "v1",
			Name:    "generic.config.ocm.software",
		},
		Configurations: []*runtime.Raw{
			{
				Type: runtime.Type{
					Name:    "custom.config",
					Version: "v1",
				},
				Data: []byte(`{"maximumNumberOfPotatoes":"100"}`),
			},
		},
	}
	writer := bytes.NewBuffer(nil)
	slog.SetDefault(slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
	ctx := t.Context()
	baseContext := context.Background()
	pm := NewPluginManager(baseContext)
	require.NoError(t, pm.RegisterPlugins(ctx, config, filepath.Join("..", "tmp", "testdata")))
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	typ, err := scheme.TypeForPrototype(&dummyv1.Repository{})
	require.NoError(t, err)
	t.Cleanup(func() {
		// make sure it's not there but during a proper shutdown now this is removed by the plugin
		_ = os.Remove("/tmp/test-plugin-plugin.socket")
	})
	proto, err := scheme.NewObject(typ)
	require.NoError(t, err)
	plugin, err := pm.ComponentVersionRepositoryRegistry.GetPlugin(ctx, proto)
	require.NoError(t, err)
	require.NoError(t, pm.Shutdown(ctx))
	// we need some time for the logs to be streamed back
	require.Eventually(t, func() bool {
		err := plugin.Ping(context.Background())
		return err != nil
	}, 1*time.Second, 100*time.Millisecond)

	content, err := io.ReadAll(writer)
	require.NoError(t, err)
	require.Contains(t, string(content), `maximumNumberOfPotatoes=100`)
}

func TestConfigurationPassedToPluginNotFound(t *testing.T) {
	config := &v1.Config{
		Type: runtime.Type{
			Version: "v1",
			Name:    "generic.config.ocm.software",
		},
		Configurations: []*runtime.Raw{},
	}
	slog.SetLogLoggerLevel(slog.LevelDebug)
	ctx := t.Context()
	baseContext := context.Background()
	pm := NewPluginManager(baseContext)
	err := pm.RegisterPlugins(ctx, config, filepath.Join("..", "tmp", "testdata"))
	require.EqualError(t, err, "failed to add plugin test-plugin: no configuration found for plugin test-plugin; requested configuration types: [custom.config/v1]")
}

func TestPluginManagerCancelContext(t *testing.T) {
	config := &v1.Config{
		Type: runtime.Type{
			Name:    "custom.config",
			Version: "v1",
		},
		Configurations: []*runtime.Raw{
			{
				Type: runtime.Type{
					Name:    "custom.config",
					Version: "v1",
				},
				Data: []byte(`{}`),
			},
		},
	}
	slog.SetLogLoggerLevel(slog.LevelDebug)
	ctx, cancel := context.WithCancel(context.Background())
	baseContext, baseCancel := context.WithCancel(context.Background()) // a different context
	pm := NewPluginManager(baseContext)
	require.NoError(t, pm.RegisterPlugins(ctx, config, filepath.Join("..", "tmp", "testdata")))
	t.Cleanup(func() {
		require.NoError(t, pm.Shutdown(ctx))
		require.NoError(t, os.Remove("/tmp/test-plugin-plugin.socket"))
	})

	proto := &dummyv1.Repository{
		Type: runtime.Type{
			Name:    "DummyRepository",
			Version: "v1",
		},
	}
	plugin, err := pm.ComponentVersionRepositoryRegistry.GetPlugin(ctx, proto)
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
	config := &v1.Config{
		Type: runtime.Type{
			Name:    "custom.config",
			Version: "v1",
		},
		Configurations: []*runtime.Raw{
			{
				Type: runtime.Type{
					Name:    "custom.config",
					Version: "v1",
				},
				Data: []byte(`{}`),
			},
		},
	}
	slog.SetLogLoggerLevel(slog.LevelDebug)
	ctx := context.Background()
	baseContext := context.Background() // a different context
	pm := NewPluginManager(baseContext)
	require.NoError(t, pm.RegisterPlugins(ctx, config, filepath.Join("..", "tmp", "testdata")))
	t.Cleanup(func() {
		// make sure it's gone even if the test fails, but ignore the deletion error since it should be removed.
		_ = os.Remove("/tmp/test-plugin-plugin.socket")
	})

	// start the plugin
	proto := &dummyv1.Repository{
		Type: runtime.Type{
			Name:    "DummyRepository",
			Version: "v1",
		},
	}
	plugin, err := pm.ComponentVersionRepositoryRegistry.GetPlugin(ctx, proto)
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
	config := &v1.Config{
		Type: runtime.Type{
			Name:    "custom.config",
			Version: "v1",
		},
		Configurations: []*runtime.Raw{
			{
				Type: runtime.Type{
					Name:    "custom.config",
					Version: "v1",
				},
				Data: []byte(`{}`),
			},
		},
	}
	writer := bytes.NewBuffer(nil)
	slog.SetDefault(slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
	ctx, cancel := context.WithCancel(context.Background())
	baseContext := context.Background() // a different context
	pm := NewPluginManager(baseContext)
	require.NoError(t, pm.RegisterPlugins(ctx, config, filepath.Join("..", "tmp", "testdata")))
	t.Cleanup(func() {
		// make sure it's gone even if the test fails, but ignore the deletion error since it should be removed.
		_ = os.Remove("/tmp/test-plugin-plugin.socket")
	})

	// start the plugin
	proto := &dummyv1.Repository{
		Type: runtime.Type{
			Name:    "DummyRepository",
			Version: "v1",
		},
	}
	plugin, err := pm.ComponentVersionRepositoryRegistry.GetPlugin(ctx, proto)
	//plugin, err := componentversionrepository.GetReadWriteComponentVersionRepositoryPluginForType(ctx, pm.ComponentVersionRepositoryRegistry, proto, scheme)
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
	require.Contains(t, string(content), "Gracefully shutting down plugin")
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
	config := &v1.Config{
		Type: runtime.Type{
			Name:    "custom.config",
			Version: "v1",
		},
	}
	require.NoError(t, pm.addPlugin(ctx, config, testPlugin, bytes.NewBuffer(serialized)))
	// trying to add the same plugin again for the same type but with different id
	// this way of testing actually showed a horrible flaw. We were passing around a pointer
	// which meant if we weren't very careful and overwrote the plugin AFTER we added it,
	// the plugin inside the map of the registry was also changed. That is really, really not desired.
	testPlugin.ID = "test-other"
	testPlugin.Path = "/tmp/test-other-plugin-plugin.socket"
	testPlugin.Config.ID = "test-other"
	testPlugin.Config.Type = "tcp"
	testPlugin.Types = nil
	require.ErrorContains(t, pm.addPlugin(ctx, config, testPlugin, bytes.NewBuffer(serialized)), "failed to register plugin test-other: plugin for type OCIRepository/v1 already registered with ID: test-id")
}

func TestPluginManagerWithNoPlugins(t *testing.T) {
	config := &v1.Config{
		Type: runtime.Type{
			Name:    "custom.config",
			Version: "v1",
		},
	}
	pm := NewPluginManager(context.Background())
	require.ErrorContains(t, pm.RegisterPlugins(context.Background(), config, filepath.Join(".")), "no plugins found")
}
