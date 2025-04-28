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

	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const socketPathFormat = "/tmp/ocm_plugin_%s.sock"

// PluginManager manages all connected plugins.
type PluginManager struct {
	// Registries containing various typed plugins. These should be called directly using the
	// plugin manager to locate a required plugin.
	ComponentVersionRepositoryRegistry *componentversionrepository.RepositoryRegistry

	mu     sync.Mutex
	logger *slog.Logger

	// baseCtx is the context that is used for all plugins.
	// This is a different context than the one used for fetching plugins because
	// that context is done once fetching is done. The plugin context, however, must not
	// be cancelled.
	baseCtx context.Context
}

// NewPluginManager initializes the PluginManager
// the passed ctx is used for all plugins.
func NewPluginManager(ctx context.Context, logger *slog.Logger) *PluginManager {
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)

	return &PluginManager{
		ComponentVersionRepositoryRegistry: componentversionrepository.NewComponentVersionRepositoryRegistry(scheme),

		baseCtx: ctx,
		logger:  logger,
	}
}

type RegistrationOptions struct {
	IdleTimeout time.Duration
}

type RegistrationOptionFn func(*RegistrationOptions)

func WithIdleTimeout(d time.Duration) RegistrationOptionFn {
	return func(o *RegistrationOptions) {
		o.IdleTimeout = d
	}
}

// RegisterPluginsAtLocation walks through files in a folder and registers them
// as plugins if connection points can be established. This function doesn't support
// concurrent access.
func (pm *PluginManager) RegisterPluginsAtLocation(ctx context.Context, dir string, opts ...RegistrationOptionFn) error {
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

	var plugins []*mtypes.Plugin
	if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// TODO: Determine plugin extension.
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

		pm.logger.DebugContext(ctx, "discovered plugin", "id", id, "path", path)

		plugins = append(plugins, p)

		return nil
	}); err != nil {
		return fmt.Errorf("failed to discover plugins: %w", err)
	}

	for _, plugin := range plugins {
		location, err := determineConnectionLocation(plugin)
		if err != nil {
			return fmt.Errorf("failed to determine connection location: %w", err)
		}
		conf.Location = location
		conf.ID = plugin.ID
		plugin.Config = *conf

		output := bytes.NewBuffer(nil)
		cmd := exec.CommandContext(ctx, cleanPath(plugin.Path), "capabilities") //nolint: gosec // G204 does not apply
		cmd.Stdout = output
		cmd.Stderr = os.Stderr

		// Use Wait so we get the capabilities and make sure that the command exists and returns the values we need.
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to start plugin %s: %w", plugin.ID, err)
		}

		types := &mtypes.Types{}
		if err := json.Unmarshal(output.Bytes(), types); err != nil {
			return fmt.Errorf("failed to unmarshal capabilities: %w", err)
		}

		serialized, err := json.Marshal(plugin.Config)
		if err != nil {
			return err
		}

		// Create a command that can then be managed.
		pluginCmd := exec.CommandContext(ctx, cleanPath(plugin.Path), "--config", string(serialized)) //nolint: gosec // G204 does not apply
		pluginCmd.Stdout = os.Stdout
		pluginCmd.Stderr = os.Stdout
		pluginCmd.Cancel = func() error {
			slog.Info("killing plugin process because the parent context is cancelled", "id", plugin.ID)
			return cmd.Process.Kill()
		}
		plugin.Cmd = pluginCmd
		plugin.Types = types.Types

		for pType, typs := range plugin.Types {
			//nolint:gocritic // will be extended later
			switch pType {
			case mtypes.ComponentVersionRepositoryPluginType:
				for _, typ := range typs {
					pm.logger.DebugContext(ctx, "transferring plugin", "id", plugin.ID)
					if err := pm.ComponentVersionRepositoryRegistry.AddPlugin(plugin, typ.Type); err != nil {
						return fmt.Errorf("failed to register plugin %s: %w", plugin.ID, err)
					}
				}
			}
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

	errs = errors.Join(errs,
		pm.ComponentVersionRepositoryRegistry.Shutdown(ctx),
	)

	return errs
}

func determineConnectionLocation(plugin *mtypes.Plugin) (_ string, err error) {
	switch plugin.Config.Type {
	case mtypes.TCP:
		listener, err := net.Listen("tcp", ":0") //nolint: gosec // G102: only does it temporarily to find an empty address
		if err != nil {
			return "", err
		}

		defer func() {
			if lerr := listener.Close(); lerr != nil {
				err = errors.Join(err, lerr)
			}
		}()

		return listener.Addr().String(), nil
	case mtypes.Socket:
		return fmt.Sprintf(socketPathFormat, plugin.ID), nil
	}

	return "", fmt.Errorf("unknown plugin connection type: %s", plugin.Config.Type)
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
