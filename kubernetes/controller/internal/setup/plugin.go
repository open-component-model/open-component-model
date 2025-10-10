// Package setup provides initialization functions for OCM components in the controller.
package setup

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
)

// PluginManagerOptions configures plugin manager initialization.
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

// NewPluginManager creates and initializes a plugin manager with the given configuration.
// It registers plugins from the configured locations and built-in plugins.
func NewPluginManager(ctx context.Context, config *genericv1.Config, opts PluginManagerOptions) (*manager.PluginManager, error) {
	if opts.Logger.GetSink() == nil {
		opts.Logger = logr.Discard()
	}

	pm := manager.NewPluginManager(ctx)
	for _, location := range opts.Locations {
		err := pm.RegisterPlugins(ctx, location,
			manager.WithIdleTimeout(opts.IdleTimeout),
		)
		if err != nil {
			// Log but don't fail - plugins are optional
			opts.Logger.V(1).Info("failed to register plugins from location",
				"location", location,
				"error", err.Error())
		}
	}

	return pm, nil
}

// ShutdownPluginManager gracefully shuts down the plugin manager.
func ShutdownPluginManager(ctx context.Context, pm *manager.PluginManager, logger logr.Logger) error {
	if pm == nil {
		return nil
	}

	if err := pm.Shutdown(ctx); err != nil {
		logger.Error(err, "failed to shutdown plugin manager")
		return fmt.Errorf("plugin manager shutdown failed: %w", err)
	}

	return nil
}
