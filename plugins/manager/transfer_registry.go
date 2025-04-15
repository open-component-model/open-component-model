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

// internalTransferPlugins contains all plugins that have been registered using internally import statement.
var internalTransferPlugins map[string]map[string]PluginBase

// RegisterInternalTransferPlugin can be called by actual implementations in the source.
// It will register any implementations directly for a given type and capability.
func RegisterInternalTransferPlugin(p PluginBase, caps []Capability) error {
	if internalTransferPlugins == nil {
		internalTransferPlugins = make(map[string]map[string]PluginBase)
	}

	for _, c := range caps {
		if internalTransferPlugins[c.Capability] == nil {
			internalTransferPlugins[c.Capability] = make(map[string]PluginBase)
		}

		if v, ok := internalTransferPlugins[c.Capability]; ok {
			if _, ok := v[c.Type]; ok {
				return fmt.Errorf("plugin for capability %s already has a type %s", c.Capability, c.Type)
			}
		}

		internalTransferPlugins[c.Capability][c.Type] = p
	}

	return nil
}

// TransferRegistry holds all plugins that implement capabilities corresponding to transfer operations.
type TransferRegistry struct {
	mu                 sync.Mutex
	registry           map[string]map[string]*Plugin
	constructedPlugins map[string]*constructedPlugin
	logger             *slog.Logger
}

// Shutdown will loop through all _STARTED_ plugins and will send an Interrupt signal to them.
// All plugins should handle interrupt signals gracefully. For Go, this is done automatically by
// the plugin SDK.
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

// AddPlugin takes a plugin discovered by the manager and adds it to the stored plugin registry.
// This function will return an error if the given capability + type already has a registered plugin.
// Multiple plugins for the same cap+typ is not allowed.
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
	// TODO: Figure out the right context here. -> Should be the base context from the plugin manager.
	repoPlugin := NewRepositoryPlugin(context.Background(), r.logger, client, plugin.ID, plugin.path, plugin.config)

	r.constructedPlugins[repoPlugin.ID] = &constructedPlugin{
		Plugin: repoPlugin,
		cmd:    plugin.cmd,
	}

	return repoPlugin, nil
}

// getInternalPlugin looks in the internally registered plugins first if we have any plugins that have
// been added.
func getInternalPlugin(capability string, typ string) (PluginBase, bool) {
	if _, ok := internalTransferPlugins[capability]; !ok {
		return nil, false
	}

	if _, ok := internalTransferPlugins[capability][typ]; !ok {
		return nil, false
	}

	return internalTransferPlugins[capability][typ], true
}

// GetReadWriteComponentVersionRepository gets a plugin that registered for this given capability.
func GetReadWriteComponentVersionRepository(ctx context.Context, pm *PluginManager, typ runtime.Typed) (ReadWriteRepositoryPluginContract, error) {
	if v, ok := getInternalPlugin(ReadWriteComponentVersionRepositoryCapability, typ.GetType().String()); ok {
		p, ok := v.(ReadWriteRepositoryPluginContract)
		if !ok {
			return nil, fmt.Errorf("read-write component version repository does not implement ReadWriteRepositoryPluginContract but was: %T", v)
		}

		return p, nil
	}

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
