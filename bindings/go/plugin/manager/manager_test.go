package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ocmrepositoryv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	pluginruntime "ocm.software/open-component-model/bindings/go/plugin/manager/types/runtime"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestPluginManager(t *testing.T) {
	config := &genericv1.Config{
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
	require.NoError(t, pm.RegisterPlugins(ctx, filepath.Join("..", "tmp", "testdata")), WithConfiguration(config))
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	typ, err := scheme.TypeForPrototype(&dummyv1.Repository{})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pm.Shutdown(ctx))
		// make sure it's not there but during a proper shutdown now this is removed by the plugin
		if err := os.Remove("/tmp/test-plugin-plugin.socket"); err != nil && !os.IsNotExist(err) {
			t.Fatal(fmt.Errorf("error was not nil and not NotFound when clearing the socket: %w", err))
		}
	})

	spec := &dummyv1.Repository{
		Type:    typ,
		BaseUrl: "https://ocm.software/test",
	}
	plugin, err := pm.ComponentVersionRepositoryRegistry.GetComponentVersionRepository(ctx, spec, nil)
	require.NoError(t, err)
	desc, err := plugin.GetComponentVersion(ctx, "test-component", "1.0.0")
	require.NoError(t, err)
	require.Equal(t, "test-component:1.0.0", desc.String())

	blob, response, err := plugin.GetLocalResource(ctx, "test-resource", "v0.0.1", nil)
	require.NoError(t, err)
	reader, err := blob.ReadCloser()
	require.NoError(t, err)
	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, "test-resource", string(content))
	require.Equal(t, "test-resource", response.Name)

	sourceBlob, sourceResponse, err := plugin.GetLocalSource(ctx, "test-source", "v0.0.1", nil)
	require.NoError(t, err)
	reader, err = sourceBlob.ReadCloser()
	require.NoError(t, err)
	content, err = io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, "test-source", string(content))
	require.Equal(t, "test-source", sourceResponse.Name)
}

func TestConfigurationPassedToPlugin(t *testing.T) {
	config := &genericv1.Config{
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
	require.NoError(t, pm.RegisterPlugins(ctx, filepath.Join("..", "tmp", "testdata"), WithConfiguration(config)))
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	typ, err := scheme.TypeForPrototype(&dummyv1.Repository{})
	require.NoError(t, err)
	t.Cleanup(func() {
		// make sure it's not there but during a proper shutdown now this is removed by the plugin
		if err := os.Remove("/tmp/test-plugin-plugin.socket"); err != nil && !os.IsNotExist(err) {
			t.Fatal(fmt.Errorf("error was not nil and not NotFound when clearing the socket: %w", err))
		}
	})
	spec := &dummyv1.Repository{
		Type:    typ,
		BaseUrl: "test",
	}
	_, err = pm.ComponentVersionRepositoryRegistry.GetComponentVersionRepository(ctx, spec, nil)
	require.NoError(t, err)

	// we need some time for the logs to be streamed back
	require.Eventually(t, func() bool {
		content := writer.String()
		return len(content) > 0 && strings.Contains(content, "maximumNumberOfPotatoes=100")
	}, 1*time.Second, 100*time.Millisecond)

	require.NoError(t, pm.Shutdown(ctx))

	// we need some time for the logs to be streamed back
	require.Eventually(t, func() bool {
		content := writer.String()
		return strings.Contains(content, `maximumNumberOfPotatoes=100`)
	}, 1*time.Second, 100*time.Millisecond)
}

func TestConfigurationPassedToPluginNotFound(t *testing.T) {
	config := &genericv1.Config{
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
	err := pm.RegisterPlugins(ctx, filepath.Join("..", "tmp", "testdata"), WithConfiguration(config))
	require.EqualError(t, err, "failed to add plugin test-plugin-component-version: no configuration found for plugin test-plugin-component-version; requested configuration types: [custom.config/v1]")
}

func TestPluginManagerCancelContext(t *testing.T) {
	config := &genericv1.Config{
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
	require.NoError(t, pm.RegisterPlugins(ctx, filepath.Join("..", "tmp", "testdata"), WithConfiguration(config)))
	t.Cleanup(func() {
		require.NoError(t, pm.Shutdown(ctx))
		// normally, this shouldn't exist because shutdown deletes now properly, but sometimes this deletes it earlier.
		if err := os.Remove("/tmp/test-plugin-component-version-plugin.socket"); err != nil && !os.IsNotExist(err) {
			t.Fatal(fmt.Errorf("error was not nil and not NotFound when clearing the socket: %w", err))
		}
	})

	proto := &dummyv1.Repository{
		Type: runtime.Type{
			Name:    "DummyRepository",
			Version: "v1",
		},
	}
	plugin, err := pm.ComponentVersionRepositoryRegistry.GetComponentVersionRepository(ctx, proto, nil)
	require.NoError(t, err)
	_, err = plugin.GetComponentVersion(context.Background(), "test", "v1.0.0")
	require.NoError(t, err)

	// cancelling the outer context should not shut down the plugin only the ongoing request.
	t.Log("cancelling outer context")
	cancel()
	_, err = plugin.GetComponentVersion(context.Background(), "test", "v1.0.0")
	require.NoError(t, err)
	//require.NoError(t, plugin.Ping(context.Background()))
	t.Log("plugin is still alive, cancelling plugin context")
	baseCancel()
	require.Eventually(t, func() bool {
		_, err = plugin.GetComponentVersion(context.Background(), "test", "v1.0.0")
		return err != nil
	}, 1*time.Second, 100*time.Millisecond)
	t.Log("plugin is stopped")
}

func TestPluginManagerShutdownPlugin(t *testing.T) {
	config := &genericv1.Config{
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
	require.NoError(t, pm.RegisterPlugins(ctx, filepath.Join("..", "tmp", "testdata"), WithConfiguration(config)))
	t.Cleanup(func() {
		// make sure it's gone even if the test fails, but ignore the deletion error since it should be removed.
		if err := os.Remove("/tmp/test-plugin-plugin.socket"); err != nil && !os.IsNotExist(err) {
			t.Fatal(fmt.Errorf("error was not nil and not NotFound when clearing the socket: %w", err))
		}
	})

	// start the plugin
	proto := &dummyv1.Repository{
		Type: runtime.Type{
			Name:    "DummyRepository",
			Version: "v1",
		},
	}
	plugin, err := pm.ComponentVersionRepositoryRegistry.GetComponentVersionRepository(ctx, proto, nil)
	require.NoError(t, err)
	_, err = plugin.GetComponentVersion(context.Background(), "test", "v1.0.0")
	require.NoError(t, err)

	// cancelling the outer context should not shut down the plugin only the ongoing request.
	require.NoError(t, pm.Shutdown(ctx))
	require.Eventually(t, func() bool {
		_, err = plugin.GetComponentVersion(context.Background(), "test", "v1.0.0")
		return err != nil
	}, 1*time.Second, 100*time.Millisecond)
	_, err = os.Stat("/tmp/test-plugin-plugin.socket")
	require.Error(t, err)
}

func TestPluginManagerShutdownWithoutWait(t *testing.T) {
	config := &genericv1.Config{
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
	require.NoError(t, pm.RegisterPlugins(ctx, filepath.Join("..", "tmp", "testdata"), WithConfiguration(config)))
	t.Cleanup(func() {
		// make sure it's gone even if the test fails, but ignore the deletion error since it should be removed.
		if err := os.Remove("/tmp/test-plugin-plugin.socket"); err != nil && !os.IsNotExist(err) {
			t.Fatal(fmt.Errorf("error was not nil and not NotFound when clearing the socket: %w", err))
		}
	})

	// start the plugin
	proto := &dummyv1.Repository{
		Type: runtime.Type{
			Name:    "DummyRepository",
			Version: "v1",
		},
	}
	plugin, err := pm.ComponentVersionRepositoryRegistry.GetComponentVersionRepository(ctx, proto, nil)
	//plugin, err := componentversionrepository.GetReadWriteComponentVersionRepositoryPluginForType(ctx, pm.ComponentVersionRepositoryRegistry, proto, scheme)
	require.NoError(t, err)
	_, err = plugin.GetComponentVersion(context.Background(), "test", "v1.0.0")
	require.NoError(t, err)

	// Cancelling the shutdown context still should allow a graceful shutdown of the plugin because
	// that context is outside of this. This shutdown will just send an interrupt signal to the
	// underlying command.
	require.NoError(t, pm.Shutdown(ctx))
	cancel()

	require.Eventually(t, func() bool {
		_, err = plugin.GetComponentVersion(context.Background(), "test", "v1.0.0")
		return err != nil
	}, 1*time.Second, 100*time.Millisecond)

	// we need some time for the logs to be streamed back
	require.Eventually(t, func() bool {
		content, err := io.ReadAll(writer)
		if err != nil {
			return false
		}
		return strings.Contains(string(content), "gracefully shutting down plugin")
	}, 1*time.Second, 100*time.Millisecond)
}

func TestPluginManagerMultiplePluginsForSameType(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	ctx := t.Context()
	baseContext := context.Background()
	pm := NewPluginManager(baseContext)
	capabilitySpec := ocmrepositoryv1.CapabilitySpec{
		Type: runtime.NewUnversionedType(string(ocmrepositoryv1.ComponentVersionRepositoryPluginType)),
		SupportedRepositorySpecTypes: []types.Type{
			{
				Type: runtime.Type{
					Name:    "OCIRepository",
					Version: "v1",
				},
				Aliases:    nil,
				JSONSchema: nil,
			},
		},
	}
	pluginSpec := pluginruntime.PluginSpec{
		CapabilitySpecs:      []runtime.Typed{&capabilitySpec},
		SupportedConfigTypes: nil,
	}
	rawPluginSpec, err := pluginruntime.ConvertToSpec(&pluginSpec)
	require.NoError(t, err)

	testPlugin := types.Plugin{
		ID:   "test-id",
		Path: "/tmp/test-plugin-plugin.socket",
		Config: types.Config{
			ID:         "test-id",
			Type:       "unix",
			PluginType: ocmrepositoryv1.ComponentVersionRepositoryPluginType,
		},
	}
	serialized, err := json.Marshal(rawPluginSpec)
	require.NoError(t, err)
	config := &genericv1.Config{
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
	require.ErrorContains(t, pm.addPlugin(ctx, config, testPlugin, bytes.NewBuffer(serialized)), "failed to register plugin test-other: plugin for type OCIRepository/v1 already registered with ID: test-id")
}

func TestPluginManagerWithNoPlugins(t *testing.T) {
	pm := NewPluginManager(context.Background())
	require.ErrorContains(t, pm.RegisterPlugins(context.Background(), filepath.Join(".")), "no plugins found")
}
