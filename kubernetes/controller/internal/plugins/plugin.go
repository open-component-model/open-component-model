// Package plugins provides initialization functions for OCM components in the controller.
package plugins

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"

	"ocm.software/open-component-model/bindings/go/plugin/manager"
)

type PluginManagerOptions struct {
	Locations   []string
	IdleTimeout time.Duration
	Logger      logr.Logger
}

// DefaultPluginManagerOptions returns default options for plugin manager setup.
func DefaultPluginManagerOptions(log logr.Logger) PluginManagerOptions {
	return PluginManagerOptions{
		Locations:   []string{}, // TODO: Set up temp?
		IdleTimeout: time.Hour,
		Logger:      log,
	}
}

// PluginManager contains settings for the plugin manager.
type PluginManager struct {
	IdleTimeout   time.Duration
	Logger        logr.Logger
	Locations     []string
	PluginManager *manager.PluginManager
}

// Start will start the plugin manager with the given configuration. Start will BLOCK until the
// context is cancelled. It's designed to be called by the controller manager.
func (m *PluginManager) Start(ctx context.Context) error {
	pm := manager.NewPluginManager(ctx)
	for _, location := range m.Locations {
		err := pm.RegisterPlugins(ctx, location,
			manager.WithIdleTimeout(m.IdleTimeout),
		)
		if err != nil {
			// Log but don't fail - plugins are optional
			m.Logger.V(1).Info("failed to register plugins from location",
				"location", location,
				"error", err.Error())
		}
	}

	m.PluginManager = pm

	<-ctx.Done() // block until context is done ( expected by the manager )

	// We use context background here because the Start context has been cancelled.
	return pm.Shutdown(context.Background())
}

func (m *PluginManager) Shutdown(ctx context.Context) error {
	if m.PluginManager == nil {
		return nil
	}

	if err := m.PluginManager.Shutdown(ctx); err != nil {
		m.Logger.Error(err, "failed to shutdown plugin manager")
		return fmt.Errorf("plugin manager shutdown failed: %w", err)
	}

	return nil
}

// NewPluginManager creates and initializes a plugin manager with the given configuration.
// It registers plugins from the configured Locations and built-in plugins.
func NewPluginManager(opts PluginManagerOptions) *PluginManager {
	if opts.Logger.GetSink() == nil {
		opts.Logger = logr.Discard()
	}

	return &PluginManager{
		Logger:      opts.Logger,
		Locations:   opts.Locations,
		IdleTimeout: opts.IdleTimeout,
	}
}
