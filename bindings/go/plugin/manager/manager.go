package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	v1 "ocm.software/open-component-model/bindings/go/configuration/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/inputrepository"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

// ErrNoPluginsFound is returned when a register plugin call finds no plugins.
var ErrNoPluginsFound = errors.New("no plugins found")

// PluginManager manages all connected plugins.
type PluginManager struct {
	// Registries containing various typed plugins. These should be called directly using the
	// plugin manager to locate a required plugin.
	ComponentVersionRepositoryRegistry *componentversionrepository.RepositoryRegistry
	CredentialRepositoryRegistry       *credentialrepository.RepositoryRegistry
	InputRegistry                      *inputrepository.RepositoryRegistry
	DigestProcessorRegistry            *digestprocessor.RepositoryRegistry

	mu sync.Mutex

	// baseCtx is the context that is used for all plugins.
	// This is a different context than the one used for fetching plugins because
	// that context is done once fetching is done. The plugin context, however, must not
	// be cancelled.
	baseCtx context.Context
}

// NewPluginManager initializes the PluginManager
// the passed ctx is used for all plugins.
func NewPluginManager(ctx context.Context) *PluginManager {
	return &PluginManager{
		ComponentVersionRepositoryRegistry: componentversionrepository.NewComponentVersionRepositoryRegistry(ctx),
		CredentialRepositoryRegistry:       credentialrepository.NewCredentialRepositoryRegistry(ctx),
		InputRegistry:                      inputrepository.NewInputRepositoryRegistry(ctx),
		DigestProcessorRegistry:            digestprocessor.NewDigestProcessorRegistry(ctx),
		baseCtx:                            ctx,
	}
}

type RegistrationOptions struct {
	IdleTimeout time.Duration
	Config      *v1.Config
}

type RegistrationOptionFn func(*RegistrationOptions)

// WithIdleTimeout configures the maximum amount of time for a plugin to quit if it's idle.
func WithIdleTimeout(d time.Duration) RegistrationOptionFn {
	return func(o *RegistrationOptions) {
		o.IdleTimeout = d
	}
}

// WithConfiguration adds a configuration to the plugin.
func WithConfiguration(c *v1.Config) RegistrationOptionFn {
	return func(o *RegistrationOptions) {
		o.Config = c
	}
}

// RegisterPlugins walks through files in a folder and registers them
// as plugins if connection points can be established. This function doesn't support
// concurrent access.
func (pm *PluginManager) RegisterPlugins(ctx context.Context, dir string, opts ...RegistrationOptionFn) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	defaultOpts := &RegistrationOptions{
		IdleTimeout: time.Hour,
	}

	for _, opt := range opts {
		opt(defaultOpts)
	}

	conf := &mtypes.Config{
		IdleTimeout: &defaultOpts.IdleTimeout,
	}

	t, err := determineConnectionType()
	if err != nil {
		return fmt.Errorf("could not determine connection type: %w", err)
	}
	conf.Type = t

	plugins, err := pm.fetchPlugins(ctx, conf, dir)
	if err != nil {
		return fmt.Errorf("could not fetch plugins: %w", err)
	}

	if len(plugins) == 0 {
		return ErrNoPluginsFound
	}

	for _, plugin := range plugins {
		conf.ID = plugin.ID
		plugin.Config = *conf

		output := bytes.NewBuffer(nil)
		cmd := exec.CommandContext(ctx, cleanPath(plugin.Path), "capabilities") //nolint:gosec // G204 does not apply
		cmd.Stdout = output
		cmd.Stderr = os.Stderr

		// Use Wait so we get the capabilities and make sure that the command exists and returns the values we need.
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to start plugin %s: %w", plugin.ID, err)
		}

		if err := pm.addPlugin(pm.baseCtx, defaultOpts.Config, *plugin, output); err != nil {
			return fmt.Errorf("failed to add plugin %s: %w", plugin.ID, err)
		}
	}

	return nil
}

func cleanPath(path string) string {
	return strings.Trim(path, `,;:'"|&*!@#$`)
}

// Shutdown is called to terminate all plugins.
func (pm *PluginManager) Shutdown(ctx context.Context) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	var errs error

	errs = errors.Join(errs, pm.ComponentVersionRepositoryRegistry.Shutdown(ctx))

	return errs
}

func (pm *PluginManager) fetchPlugins(ctx context.Context, conf *mtypes.Config, dir string) ([]*mtypes.Plugin, error) {
	var plugins []*mtypes.Plugin
	if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if os.IsNotExist(err) {
			return ErrNoPluginsFound
		}

		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// TODO(Skarlso): Determine plugin extension.
		ext := filepath.Ext(info.Name())
		if ext != "" {
			return nil
		}

		id := filepath.Base(path)

		p := &mtypes.Plugin{
			ID:     id,
			Path:   path,
			Config: *conf,
		}

		slog.DebugContext(ctx, "discovered plugin", "id", id, "path", path)

		plugins = append(plugins, p)

		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to discover plugins: %w", err)
	}

	return plugins, nil
}

func (pm *PluginManager) addPlugin(ctx context.Context, ocmConfig *v1.Config, plugin mtypes.Plugin, capabilitiesCommandOutput *bytes.Buffer) error {
	// Determine Configuration requirements.
	types := &mtypes.Types{}
	if err := json.Unmarshal(capabilitiesCommandOutput.Bytes(), types); err != nil {
		return fmt.Errorf("failed to unmarshal capabilities: %w", err)
	}

	if ocmConfig != nil {
		filtered, _ := v1.Filter(ocmConfig, &v1.FilterOptions{ConfigTypes: types.ConfigTypes})
		if len(types.ConfigTypes) > 0 && len(filtered.Configurations) == 0 {
			return fmt.Errorf("no configuration found for plugin %s; requested configuration types: %s", plugin.ID, types.ConfigTypes)
		}

		plugin.Config.ConfigTypes = append(plugin.Config.ConfigTypes, filtered.Configurations...)
	}

	serialized, err := json.Marshal(plugin.Config)
	if err != nil {
		return err
	}

	// Create a command that can then be managed.
	pluginCmd := exec.CommandContext(ctx, cleanPath(plugin.Path), "--config", string(serialized)) //nolint:gosec // G204 does not apply
	pluginCmd.Cancel = func() error {
		slog.Info("killing plugin process because the parent context is cancelled", "id", plugin.ID)
		return pluginCmd.Process.Kill()
	}

	// Set up communication pipes.
	plugin.Cmd = pluginCmd
	sdtErr, err := pluginCmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}
	plugin.Stderr = sdtErr
	sdtOut, err := pluginCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	plugin.Stdout = sdtOut
	plugin.Types = types.Types

	for pType, typs := range plugin.Types {
		switch pType {
		case mtypes.ComponentVersionRepositoryPluginType:
			for _, typ := range typs {
				slog.DebugContext(ctx, "adding component version repository plugin", "id", plugin.ID)
				if err := pm.ComponentVersionRepositoryRegistry.AddPlugin(plugin, typ.Type); err != nil {
					return fmt.Errorf("failed to register plugin %s: %w", plugin.ID, err)
				}
			}
		case mtypes.CredentialRepositoryPluginType:
			slog.DebugContext(ctx, "adding credential repository plugin", "id", plugin.ID)
			if err := pm.CredentialRepositoryRegistry.AddPlugin(plugin, typs[0].Type, typs[1].Type); err != nil {
				return fmt.Errorf("failed to register plugin %s: %w", plugin.ID, err)
			}
		case mtypes.InputPluginType:
			slog.DebugContext(ctx, "adding construction resource input plugin", "id", plugin.ID)
			if err := pm.InputRegistry.AddPlugin(plugin, typs[0].Type); err != nil {
				return fmt.Errorf("failed to register plugin %s: %w", plugin.ID, err)
			}
		case mtypes.DigestProcessorPluginType:
			slog.DebugContext(ctx, "adding digest processor plugin", "id", plugin.ID)
			if err := pm.DigestProcessorRegistry.AddPlugin(plugin, typs[0].Type); err != nil {
				return fmt.Errorf("failed to register plugin %s: %w", plugin.ID, err)
			}
		}
	}

	return nil
}

func determineConnectionType() (mtypes.ConnectionType, error) {
	tmp, err := os.MkdirTemp("", "")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary directory: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tmp)
	}()

	socketPath := filepath.Join(tmp, "plugin.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return mtypes.TCP, nil
	}

	if err := listener.Close(); err != nil {
		return "", fmt.Errorf("failed to close socket: %w", err)
	}

	return mtypes.Socket, nil
}
