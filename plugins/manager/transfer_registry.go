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

	for _, c := range caps.Capabilities {
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

// Only external methods like the generatic ones like in POCM. -> get transfer plugin for type and then the
// Like in POCM the type is the only input because the capability is specific to the plugin type to context/action.
// Capability is not needed since we already have that from the context.
func (r *TransferRegistry) GetPlugin(ctx context.Context, capability, typ string) (any, error) {
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

// GetTransferPlugin fetches a plugin from the transfer registry.
func GetTransferPlugin[T PluginBase](ctx context.Context, pm *PluginManager, capability string, typ runtime.Typed) (T, error) {
	var t T
	p, err := pm.TransferRegistry.GetPlugin(ctx, capability, typ.GetType().String())
	if err != nil {
		return t, fmt.Errorf("error getting transfer plugin for capability %s with type %s: %w", capability, typ.GetType().String(), err)
	}

	t, ok := p.(T)
	if !ok {
		return t, fmt.Errorf("transfer plugin for capability %s does not implement T interface", capability)
	}

	return t, nil
}

func NewTransferRegistry() *TransferRegistry {
	return &TransferRegistry{
		registry:           make(map[string]map[string]*Plugin),
		constructedPlugins: make(map[string]*constructedPlugin),
	}
}
