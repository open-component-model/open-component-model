package componentversionrepository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type constructedPlugin struct {
	Plugin contracts.PluginBase

	cmd *exec.Cmd
}

// RegisterInternalComponentVersionRepositoryPlugin can be called by actual implementations in the source.
// It will register any implementations directly for a given type and capability.
func RegisterInternalComponentVersionRepositoryPlugin[T runtime.Typed](scheme *runtime.Scheme, r *RepositoryRegistry, p v1.ReadWriteOCMRepositoryPluginContract[T], prototype T) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	typ, err := scheme.TypeForPrototype(prototype)
	if err != nil {
		return fmt.Errorf("failed to get type for prototype %T: %w", prototype, err)
	}

	r.internalComponentVersionRepositoryPlugins[typ] = p

	if err := r.internalComponentVersionRepositoryScheme.RegisterWithAlias(prototype, typ); err != nil {
		return fmt.Errorf("failed to register prototype %T: %w", prototype, err)
	}

	return nil
}

// RepositoryRegistry holds all plugins that implement capabilities corresponding to RepositoryPlugin operations.
type RepositoryRegistry struct {
	ctx                context.Context
	mu                 sync.Mutex
	registry           map[runtime.Type]mtypes.Plugin // Have this as a single plugin for read/write
	constructedPlugins map[string]*constructedPlugin  // running plugins

	// internalComponentVersionRepositoryPlugins contains all plugins that have been registered using internally import statement.
	internalComponentVersionRepositoryPlugins map[runtime.Type]contracts.PluginBase
	// internalComponentVersionRepositoryScheme is the holder of schemes. This hold will contain the scheme required to
	// construct and understand the passed in types and what / how they need to look like. The passed in scheme during
	// registration will be added to this scheme holder. Once this happens, the code will validate any passed in objects
	// that their type is registered or not.
	internalComponentVersionRepositoryScheme *runtime.Scheme
}

// Shutdown will loop through all _STARTED_ plugins and will send an Interrupt signal to them.
// All plugins should handle interrupt signals gracefully. For Go, this is done automatically by
// the plugin SDK.
func (r *RepositoryRegistry) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var errs error
	for _, p := range r.constructedPlugins {
		// The plugins should handle the Interrupt signal for shutdowns.
		// TODO(Skarlso): Use context to wait for the plugin to actually shut down.
		if perr := p.cmd.Process.Signal(os.Interrupt); perr != nil {
			errs = errors.Join(errs, perr)
		}
	}

	return errs
}

// AddPlugin takes a plugin discovered by the manager and adds it to the stored plugin registry.
// This function will return an error if the given capability + type already has a registered plugin.
// Multiple plugins for the same cap+typ is not allowed.
func (r *RepositoryRegistry) AddPlugin(plugin mtypes.Plugin, typ runtime.Type) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if v, ok := r.registry[typ]; ok {
		return fmt.Errorf("plugin for type %v already registered with ID: %s", typ, v.ID)
	}

	// _Note_: No need to be more intricate because we know the endpoints, and we have a specific plugin here.
	r.registry[typ] = plugin

	return nil
}

// getPluginWithType returns the registered plugin with the given type.
func (r *RepositoryRegistry) getPluginWithType(typ runtime.Type) (*mtypes.Plugin, error) {
	p, ok := r.registry[typ]
	if !ok {
		return nil, fmt.Errorf("no plugin registered for type %v", typ)
	}

	return &p, nil
}

// GetReadWriteComponentVersionRepositoryPluginForType finds a specific plugin in the registry.
// On the first call, it will initialize and start the plugin. On any consecutive calls it will return the
// existing plugin that has already been started.
func GetReadWriteComponentVersionRepositoryPluginForType[T runtime.Typed](ctx context.Context, r *RepositoryRegistry, proto T, scheme *runtime.Scheme) (v1.ReadWriteOCMRepositoryPluginContract[T], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// if we find the type has been registered internally, we look for internal plugins for it.
	if typ, err := r.internalComponentVersionRepositoryScheme.TypeForPrototype(proto); err == nil {
		return getInternalPlugin[T](r, typ)
	}

	typ, err := scheme.TypeForPrototype(proto)
	if err != nil {
		return nil, fmt.Errorf("failed to get type for prototype %T: %w", proto, err)
	}

	plugin, err := r.getPluginWithType(typ)
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin for typ %T %s: %w", typ, typ, err)
	}

	if existingPlugin, ok := r.constructedPlugins[plugin.ID]; ok {
		pt, ok := existingPlugin.Plugin.(v1.ReadWriteOCMRepositoryPluginContract[T])
		if !ok {
			return nil, fmt.Errorf("existing plugin for typ %T does not implement ReadOCMRepositoryPluginContract[T]", existingPlugin)
		}

		return pt, nil
	}

	return startAndReturnPlugin[T](ctx, r, plugin)
}

func getInternalPlugin[T runtime.Typed](r *RepositoryRegistry, typ runtime.Type) (v1.ReadWriteOCMRepositoryPluginContract[T], error) {
	p, ok := r.getInternalComponentVersionRepositoryPlugin(typ)
	if !ok {
		return nil, fmt.Errorf("type %v is registered but no plugin exists", typ)
	}

	pt, ok := p.(v1.ReadWriteOCMRepositoryPluginContract[T])
	if !ok {
		return nil, fmt.Errorf("type %v is not a ReadWriteOCMRepositoryPluginContract[T]", typ)
	}

	return pt, nil
}

func startAndReturnPlugin[T runtime.Typed](ctx context.Context, r *RepositoryRegistry, plugin *mtypes.Plugin) (v1.ReadWriteOCMRepositoryPluginContract[T], error) {
	if err := plugin.Cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start plugin: %s, %w", plugin.ID, err)
	}

	client, loc, err := plugins.WaitForPlugin(ctx, plugin)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for plugin to start: %w", err)
	}

	// start log streaming once the plugin is up and running.
	// use the baseCtx here from the manager here so the streaming isn't stopped when the request is stopped.
	go plugins.StartLogStreamer(r.ctx, plugin)

	// think about this better, we have a single json schema, maybe even have different maps for different types + schemas?
	var jsonSchema []byte
loop:
	for _, tps := range plugin.Types {
		for _, tp := range tps {
			jsonSchema = tp.JSONSchema
			break loop
		}
	}

	repoPlugin := NewComponentVersionRepositoryPlugin(client, plugin.ID, plugin.Path, plugin.Config, loc, jsonSchema)
	r.constructedPlugins[plugin.ID] = &constructedPlugin{
		Plugin: repoPlugin,
		cmd:    plugin.Cmd,
	}

	// wrap the untyped internal plugin into a typed representation.
	return NewTypedComponentVersionRepositoryPluginImplementation[T](repoPlugin), nil
}

// getInternalComponentVersionRepositoryPlugin looks in the internally registered plugins first if we have any plugins that have
// been added.
func (r *RepositoryRegistry) getInternalComponentVersionRepositoryPlugin(typ runtime.Type) (contracts.PluginBase, bool) {
	if _, ok := r.internalComponentVersionRepositoryPlugins[typ]; !ok {
		return nil, false
	}

	return r.internalComponentVersionRepositoryPlugins[typ], true
}

// NewComponentVersionRepositoryRegistry creates a new registry and initializes maps.
func NewComponentVersionRepositoryRegistry(ctx context.Context) *RepositoryRegistry {
	return &RepositoryRegistry{
		ctx:                ctx,
		registry:           make(map[runtime.Type]mtypes.Plugin),
		constructedPlugins: make(map[string]*constructedPlugin),
		internalComponentVersionRepositoryPlugins: make(map[runtime.Type]contracts.PluginBase),
		internalComponentVersionRepositoryScheme:  runtime.NewScheme(),
	}
}
