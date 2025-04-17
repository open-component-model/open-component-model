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

// internalComponentVersionRepositoryPlugins contains all plugins that have been registered using internally import statement.
var internalComponentVersionRepositoryPlugins map[runtime.Type]PluginBase

// internalComponentVersionRepositoryScheme is the holder of schemes. This hold will contain the scheme required to
// construct and understand the passed in types and what / how they need to look like. The passed in scheme during
// registration will be added to this scheme holder. Once this happens, the code will validate any passed in objects
// that their type is registered or not.
var internalComponentVersionRepositoryScheme = runtime.NewScheme()

// RegisterInternalComponentVersionRepositoryPlugin can be called by actual implementations in the source.
// It will register any implementations directly for a given type and capability.
func RegisterInternalComponentVersionRepositoryPlugin[T runtime.Typed](scheme *runtime.Scheme, p ReadWriteOCMRepositoryPluginContract[T], prototype T) error {
	if internalComponentVersionRepositoryPlugins == nil {
		internalComponentVersionRepositoryPlugins = make(map[runtime.Type]PluginBase)
	}
	typ, err := scheme.TypeForPrototype(prototype)
	if err != nil {
		return fmt.Errorf("failed to get type for prototype %T: %w", prototype, err)
	}

	internalComponentVersionRepositoryPlugins[typ] = p

	if err := internalComponentVersionRepositoryScheme.RegisterWithAlias(prototype, typ); err != nil {
		return fmt.Errorf("failed to register prototype %T: %w", prototype, err)
	}

	return nil
}

// ComponentVersionRepositoryRegistry holds all plugins that implement capabilities corresponding to ComponentVersionRepositoryPlugin operations.
type ComponentVersionRepositoryRegistry struct {
	mu                 sync.Mutex
	registry           map[runtime.Type][]*Plugin // store them based on the runtime Type and then figure out which endpoints they support?
	constructedPlugins map[string]*constructedPlugin
	logger             *slog.Logger
	scheme             *runtime.Scheme
}

// Shutdown will loop through all _STARTED_ plugins and will send an Interrupt signal to them.
// All plugins should handle interrupt signals gracefully. For Go, this is done automatically by
// the plugin SDK.
func (r *ComponentVersionRepositoryRegistry) Shutdown(ctx context.Context) error {
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
func (r *ComponentVersionRepositoryRegistry) AddPlugin(plugin *Plugin, endpoints []Endpoint) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, e := range endpoints {
		for _, t := range e.Types {
			// TODO: maybe this is fine? think about it could there be the same type?
			//if v, ok := r.registry[t.Type]; ok {
			//	return fmt.Errorf("type %s already has a registered plugin %s", t.Type, v.ID)
			//}

			// We just append the plugin to the type it supports and then later look for a plugin
			// that declared both endpoints.
			r.registry[t.Type] = append(r.registry[t.Type], plugin)
		}
	}

	return nil
}

func (r *ComponentVersionRepositoryRegistry) getPluginForEndpointsWithType(typ runtime.Type, endpoints ...string) (*Plugin, error) {
	// TODO: This might get a plugin that implemented write TOO! This lookup isn't exclusive.
	// We basically return the first plugin we find that has all the requested endpoints declared.
	// This might be okay, since calling this again, should return the same plugin. Either way, the returned plugin
	// will have the right type and the right endpoints.

	for _, p := range r.registry[typ] {
		found := true

	loop:
		for _, endpoint := range endpoints {
			for _, e := range p.endpoints[ComponentVersionRepositoryPlugin] {
				if e.Location == endpoint {
					break loop
				}
			}

			found = false
			break
		}

		if found {
			return p, nil
		}
	}

	return nil, fmt.Errorf("no plugin found for type %s", typ)
}

// GetReadComponentVersionRepositoryPluginForType finds a specific plugin the registry. Taking a capability and a type for that capability
// it will find and return a registered plugin.
// On the first call, it will initialize and start the plugin. On any consecutive calls it will return the
// existing plugin that has already been started.
func GetReadComponentVersionRepositoryPluginForType[T runtime.Typed](ctx context.Context, r *ComponentVersionRepositoryRegistry, proto T) (ReadOCMRepositoryPluginContract[T], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// TODO add internal lookup using the internal scheme registry instead

	typ, err := r.scheme.TypeForPrototype(proto)
	if err != nil {
		return nil, fmt.Errorf("failed to get type for prototype %T: %w", proto, err)
	}

	plugin, err := r.getPluginForEndpointsWithType(typ, DownloadComponentVersion, DownloadLocalResource)
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin for typ %T %s: %w", typ, typ, err)
	}

	if existingPlugin, ok := r.constructedPlugins[plugin.ID]; ok {
		pt, ok := existingPlugin.Plugin.(ReadOCMRepositoryPluginContract[T])
		if !ok {
			return nil, fmt.Errorf("existing plugin for typ %T does not implement ReadOCMRepositoryPluginContract[T]", existingPlugin)
		}
		return pt, nil
	}

	if err := plugin.cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start plugin: %s, %w", plugin.ID, err)
	}

	client, err := waitForPlugin(ctx, plugin.ID, plugin.config.Location, plugin.config.Type)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for plugin to start: %w", err)
	}

	// TODO: Figure out the right context here. -> Should be the base context from the plugin manager.
	repoPlugin := NewRepositoryPlugin[T](context.Background(), r.logger, client, plugin.ID, plugin.path, plugin.config, plugin.endpoints[ComponentVersionRepositoryPlugin])

	r.constructedPlugins[plugin.ID] = &constructedPlugin{
		Plugin: repoPlugin,
		cmd:    plugin.cmd,
	}

	return repoPlugin, nil
}

// getInternalComponentVersionRepositoryPlugin looks in the internally registered plugins first if we have any plugins that have
// been added.
func getInternalComponentVersionRepositoryPlugin(typ runtime.Type) (PluginBase, bool) {
	if _, ok := internalComponentVersionRepositoryPlugins[typ]; !ok {
		return nil, false
	}

	return internalComponentVersionRepositoryPlugins[typ], true
}

// NewComponentVersionRepositoryRegistry creates a new registry and initializes maps.
func NewComponentVersionRepositoryRegistry(scheme *runtime.Scheme) *ComponentVersionRepositoryRegistry {
	return &ComponentVersionRepositoryRegistry{
		scheme:             scheme,
		registry:           make(map[runtime.Type][]*Plugin),
		constructedPlugins: make(map[string]*constructedPlugin),
	}
}
