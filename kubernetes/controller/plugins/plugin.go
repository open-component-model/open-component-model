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
func DefaultPluginManagerOptions() PluginManagerOptions {
	return PluginManagerOptions{
		Locations:   []string{}, // TODO: Set up temp?
		IdleTimeout: time.Hour,
		Logger:      logr.Discard(),
	}
}

type PluginManager struct {
	logger      logr.Logger
	locations   []string
	idleTimeout time.Duration
	pm          *manager.PluginManager
}

func (m *PluginManager) Start(ctx context.Context) error {
	pm := manager.NewPluginManager(ctx)
	for _, location := range m.locations {
		err := pm.RegisterPlugins(ctx, location,
			manager.WithIdleTimeout(m.idleTimeout),
		)
		if err != nil {
			// Log but don't fail - plugins are optional
			m.logger.V(1).Info("failed to register plugins from location",
				"location", location,
				"error", err.Error())
		}
	}

	m.pm = pm

	// TODO: Add context lock.

	return nil
}

func (m *PluginManager) Shutdown(ctx context.Context) error {
	if m.pm == nil {
		return nil
	}

	if err := m.pm.Shutdown(ctx); err != nil {
		m.logger.Error(err, "failed to shutdown plugin manager")
		return fmt.Errorf("plugin manager shutdown failed: %w", err)
	}

	return nil
}

func (m *PluginManager) PluginManager() *manager.PluginManager {
	return m.pm
}

// NewPluginManager creates and initializes a plugin manager with the given configuration.
// It registers plugins from the configured locations and built-in plugins.
func NewPluginManager(opts PluginManagerOptions) *PluginManager {
	if opts.Logger.GetSink() == nil {
		opts.Logger = logr.Discard()
	}

	return &PluginManager{
		logger:      logr.Logger{},
		locations:   opts.Locations,
		idleTimeout: opts.IdleTimeout,
	}
}
