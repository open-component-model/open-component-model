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
)

const socketPathFormat = "/tmp/ocm_plugin_%s.sock"

// PluginType defines the type of the plugin such as, Transfer, Transformation, Credential, Config plugin.
type PluginType string

var (
	TransformationPlugin PluginType = "transformation"
	TransferPlugin       PluginType = "transfer"
	CredentialPlugin     PluginType = "credential"
)

// Plugin represents a connected plugin
type Plugin struct {
	ID           string
	path         string
	config       Config
	capabilities map[PluginType][]Capability

	cmd *exec.Cmd
}

// PluginManager manages all connected plugins.
type PluginManager struct {
	// Registries containing various typed plugins. These should be called directly using the
	// plugin manager to locate a required plugin.
	TransferRegistry       *TransferRegistry
	TransformationRegistry *TransformationRegistry
	CredentialRegistry     *CredentialRegistry

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
	return &PluginManager{
		TransformationRegistry: NewTransformationRegistry(),
		TransferRegistry:       NewTransferRegistry(),
		CredentialRegistry:     NewCredentialRegistry(),

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

	conf := &Config{
		IdleTimeout: &defaultOpts.IdleTimeout,
	}

	t, err := determineConnectionType()
	if err != nil {
		return fmt.Errorf("could not determine connection type: %w", err)
	}
	conf.Type = t

	var plugins []*Plugin
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

		p := &Plugin{
			ID:     id,
			path:   path,
			config: *conf,
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
		plugin.config = *conf

		output := bytes.NewBuffer(nil)
		cmd := exec.CommandContext(ctx, cleanPath(plugin.path), "capabilities") //nolint: gosec // G204 does not apply
		cmd.Stdout = output
		cmd.Stderr = os.Stderr

		// Use Wait so we get the capabilities and make sure that the command exists and returns the values we need.
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to start plugin %s: %w", plugin.ID, err)
		}

		caps := &Capabilities{}
		if err := json.Unmarshal(output.Bytes(), caps); err != nil {
			return fmt.Errorf("failed to unmarshal capabilities: %w", err)
		}

		serialized, err := json.Marshal(plugin.config)
		if err != nil {
			return err
		}

		// Create a command that can then be managed.
		pluginCmd := exec.CommandContext(ctx, cleanPath(plugin.path), "--config", string(serialized)) //nolint: gosec // G204 does not apply
		pluginCmd.Stdout = os.Stdout
		pluginCmd.Stderr = os.Stdout
		pluginCmd.Cancel = func() error {
			slog.Info("killing plugin process because the parent context is cancelled", "id", plugin.ID)
			return cmd.Process.Kill()
		}
		plugin.cmd = pluginCmd
		plugin.capabilities = caps.Capabilities

		// TODO: Inbuilt stuff still needs to work. For example OCI one.
		// For all plugin types of this binary, add the plugin to the right registry
		for pType := range plugin.capabilities {
			switch pType {
			case TransferPlugin:
				pm.logger.DebugContext(ctx, "transferring plugin", "id", plugin.ID)
				if err := pm.TransferRegistry.AddPlugin(plugin, caps); err != nil {
					return fmt.Errorf("failed to register plugin %s: %w", plugin.ID, err)
				}
			case CredentialPlugin:
			case TransformationPlugin:
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
		pm.TransferRegistry.Shutdown(ctx),
		pm.TransformationRegistry.Shutdown(ctx),
		pm.CredentialRegistry.Shutdown(ctx),
	)

	return errs
}

func determineConnectionLocation(plugin *Plugin) (_ string, err error) {
	switch plugin.config.Type {
	case TCP:
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
	case Socket:
		return fmt.Sprintf(socketPathFormat, plugin.ID), nil
	}

	return "", fmt.Errorf("unknown plugin connection type: %s", plugin.config.Type)
}

func determineConnectionType() (ConnectionType, error) {
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
		return TCP, nil
	}

	if err := listener.Close(); err != nil {
		return "", fmt.Errorf("failed to close socket: %w", err)
	}

	return Socket, nil
}
