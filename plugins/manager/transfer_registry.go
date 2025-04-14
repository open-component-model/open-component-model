package manager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// The registry has the right type to return so no need for the generic interfaces.
// Meaning the Interface that declares the plugin's capabilities should be defined
// in here and then just return the Plugin itself.
type TransferRegistry struct {
	mu                 sync.Mutex
	registry           map[string]map[string]*Plugin
	constructedPlugins map[string]*constructedPlugin
	logger             *slog.Logger
}

func (r *TransferRegistry) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var errs error
	for _, p := range r.constructedPlugins {
		// The plugins should handle the Interrupt signal for shutdowns.
		// TODO: Use context to wait for the plugin to actually shut down.
		if perr := p.cmd.Process.Signal(os.Interrupt); perr != nil {
			errs = errors.Join(errs, perr)
		}
	}

	return errs
}

func (r *TransferRegistry) AddPlugin(plugin *Plugin, caps *Capabilities) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// This is a specific transfer plugin with a specific type we _know_.
	for _, c := range caps.Capabilities[TransferPlugin] {
		if r.registry[c.Capability] == nil {
			r.registry[c.Capability] = make(map[string]*Plugin)
		}

		if v, ok := r.registry[c.Capability]; ok {
			if p, ok := v[c.Type]; ok {
				return fmt.Errorf("plugin for capability %s already has a type %s with plugin ID: %s", c.Capability, c.Type, p.ID)
			}
		}

		r.registry[c.Capability][c.Type] = plugin
	}

	return nil
}

// GetPlugin finds a specific plugin the registry. Taking a capability and a type for that capability
// it will find and return a registered plugin.
// On the first call, it will initialize and start the plugin. On any consecutive calls it will return the
// existing plugin that has already been started.
func (r *TransferRegistry) GetPlugin(ctx context.Context, capability, typ string) (*RepositoryPlugin, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.registry[capability]; !ok {
		return nil, fmt.Errorf("transfer plugin for capability %s not found", capability)
	}

	if _, ok := r.registry[capability][typ]; !ok {
		return nil, fmt.Errorf("transfer plugin for typ %s not found", typ)
	}

	plugin := r.registry[capability][typ]
	if existingPlugin, ok := r.constructedPlugins[plugin.ID]; ok {
		return existingPlugin.Plugin, nil
	}

	if err := plugin.cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start plugin: %s, %w", plugin.ID, err)
	}

	client, err := waitForPlugin(ctx, plugin.ID, plugin.config.Location, plugin.config.Type)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for plugin to start: %w", err)
	}

	// create the base plugin backed by a concrete implementation of plugin interfaces.
	// TODO: Figure out the right context here.
	repoPlugin := NewRepositoryPlugin(context.Background(), r.logger, client, plugin.ID, plugin.path, plugin.config)

	r.constructedPlugins[repoPlugin.ID] = &constructedPlugin{
		Plugin: repoPlugin,
		cmd:    plugin.cmd,
	}

	return repoPlugin, nil
}

// GetReadWriteComponentVersionRepository gets a plugin that registered for this given capability.
func GetReadWriteComponentVersionRepository(ctx context.Context, pm *PluginManager, typ runtime.Typed) (ReadWriteRepositoryPluginContract, error) {
	p, err := pm.TransferRegistry.GetPlugin(ctx, ReadWriteComponentVersionRepositoryCapability, typ.GetType().String())
	if err != nil {
		return nil, fmt.Errorf("error getting transfer plugin for capability %s with type %s: %w", ReadWriteComponentVersionRepositoryCapability, typ.GetType().String(), err)
	}

	return p, nil
}

// NewTransferRegistry creates a new registry and initializes maps.
func NewTransferRegistry() *TransferRegistry {
	return &TransferRegistry{
		registry:           make(map[string]map[string]*Plugin),
		constructedPlugins: make(map[string]*constructedPlugin),
	}
}
