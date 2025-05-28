package constructorrepositroy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts/construction/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// NewConstructionRepositoryRegistry creates a new registry and initializes maps.
func NewConstructionRepositoryRegistry(ctx context.Context) *RepositoryRegistry {
	return &RepositoryRegistry{
		ctx:                                              ctx,
		resourceInputRegistry:                            make(map[runtime.Type]mtypes.Plugin),
		sourceInputRegistry:                              make(map[runtime.Type]mtypes.Plugin),
		resourceDigestProcessorRegistry:                  make(map[runtime.Type]mtypes.Plugin),
		constructedPlugins:                               make(map[string]*constructedPlugin), // running plugins
		internalResourceInputRepositoryPlugins:           make(map[runtime.Type]v1.ConstructionContract),
		internalResourceInputRepositoryScheme:            runtime.NewScheme(),
		internalSourceInputRepositoryPlugins:             make(map[runtime.Type]v1.ConstructionContract),
		internalSourceInputRepositoryScheme:              runtime.NewScheme(),
		internalResourceDigestProcessorRepositoryPlugins: make(map[runtime.Type]v1.ConstructionContract),
		internalResourceDigestProcessorRepositoryScheme:  runtime.NewScheme(),
	}
}

// RepositoryRegistry holds all plugins that implement capabilities corresponding to RepositoryPlugin operations.
type RepositoryRegistry struct {
	ctx                             context.Context
	mu                              sync.Mutex
	resourceInputRegistry           map[runtime.Type]mtypes.Plugin
	sourceInputRegistry             map[runtime.Type]mtypes.Plugin
	resourceDigestProcessorRegistry map[runtime.Type]mtypes.Plugin
	constructedPlugins              map[string]*constructedPlugin // running plugins

	// TODO: Think about this being a list instead where the elements are a struct{contract, type}.
	// And then the list would have a lookup method for Has/Exists. Might be better codewise. It's no like we
	// have thousands of plugins to loop through.
	internalResourceInputRepositoryPlugins           map[runtime.Type]v1.ConstructionContract
	internalSourceInputRepositoryPlugins             map[runtime.Type]v1.ConstructionContract
	internalResourceDigestProcessorRepositoryPlugins map[runtime.Type]v1.ConstructionContract

	internalResourceInputRepositoryScheme           *runtime.Scheme
	internalSourceInputRepositoryScheme             *runtime.Scheme
	internalResourceDigestProcessorRepositoryScheme *runtime.Scheme
}

// TODO: Think about this.
// func (r *RepositoryRegistry) RepositoryScheme() *runtime.Scheme {
//	 return r.internalConstructionRepositoryScheme
// }

// AddResourceInputPlugin takes a construction discovered by the manager and adds it to the stored construction registry.
func (r *RepositoryRegistry) AddResourceInputPlugin(plugin mtypes.Plugin, constructionType runtime.Type) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if plugin, ok := r.resourceInputRegistry[constructionType]; ok {
		return fmt.Errorf("plugin for construction type %q already registered with ID: %s", constructionType, plugin.ID)
	}

	r.resourceInputRegistry[constructionType] = plugin

	return nil
}

func (r *RepositoryRegistry) GetResourceInputPlugin(ctx context.Context, spec runtime.Typed) (v1.ResourceInputPluginContract, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, err := r.internalResourceInputRepositoryScheme.DefaultType(spec); err != nil {
		return nil, fmt.Errorf("failed to default type for prototype %T: %w", spec, err)
	}
	// if we find the type has been registered internally, we look for internal plugins for it.
	if typ, err := r.internalResourceInputRepositoryScheme.TypeForPrototype(spec); err == nil {
		p, ok := r.internalResourceInputRepositoryPlugins[typ]
		if !ok {
			return nil, fmt.Errorf("no internal plugin registered for type %v", typ)
		}

		return p, nil
	}

	plugin, err := r.getPlugin(ctx, r.resourceInputRegistry, spec)
	if err != nil {
		return nil, err
	}

	return plugin, nil
}

func (r *RepositoryRegistry) GetSourceInputPlugin(ctx context.Context, spec runtime.Typed) (v1.SourceInputPluginContract, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, err := r.internalSourceInputRepositoryScheme.DefaultType(spec); err != nil {
		return nil, fmt.Errorf("failed to default type for prototype %T: %w", spec, err)
	}
	// if we find the type has been registered internally, we look for internal plugins for it.
	if typ, err := r.internalSourceInputRepositoryScheme.TypeForPrototype(spec); err == nil {
		p, ok := r.internalSourceInputRepositoryPlugins[typ]
		if !ok {
			return nil, fmt.Errorf("no internal plugin registered for type %v", typ)
		}

		return p, nil
	}

	plugin, err := r.getPlugin(ctx, r.sourceInputRegistry, spec)
	if err != nil {
		return nil, err
	}

	return plugin, nil
}

func (r *RepositoryRegistry) GetResourceDigestProcessorPlugin(ctx context.Context, spec runtime.Typed) (v1.ResourceDigestProcessorPlugin, error) {
	if _, err := r.internalResourceDigestProcessorRepositoryScheme.DefaultType(spec); err != nil {
		return nil, fmt.Errorf("failed to default type for prototype %T: %w", spec, err)
	}
	// if we find the type has been registered internally, we look for internal plugins for it.
	if typ, err := r.internalResourceDigestProcessorRepositoryScheme.TypeForPrototype(spec); err == nil {
		p, ok := r.internalResourceDigestProcessorRepositoryPlugins[typ]
		if !ok {
			return nil, fmt.Errorf("no internal plugin registered for type %v", typ)
		}

		return p, nil
	}

	plugin, err := r.getPlugin(ctx, r.resourceDigestProcessorRegistry, spec)
	if err != nil {
		return nil, err
	}

	return plugin, nil
}

// Loops through all the plugin with the same type implementing the right contract. Because there could be
// multiple plugins, with the same type but implementing a different contract. For example,
// a ResourceInputPlugin and the SourceInputPlugin but registered for the same type.
func (r *RepositoryRegistry) getPlugin(ctx context.Context, registry map[runtime.Type]mtypes.Plugin, spec runtime.Typed) (v1.ConstructionContract, error) {
	// if we don't find the type registered internally, we look for external plugins by using the type
	// from the specification.
	typ := spec.GetType()
	if typ.IsEmpty() {
		return nil, fmt.Errorf("external plugins can not be fetched without a type %T", spec)
	}

	plugin, ok := registry[typ]
	if !ok {
		return nil, fmt.Errorf("failed to get plugin for typ %q", typ)
	}

	if existingPlugin, ok := r.constructedPlugins[plugin.ID]; ok {
		return existingPlugin.Plugin, nil
	}

	return startAndReturnPlugin(ctx, r, &plugin)
}

func RegisterInternalResourceInputPlugin(
	scheme *runtime.Scheme,
	r *RepositoryRegistry,
	plugin v1.ConstructionContract,
	proto runtime.Typed,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	typ, err := scheme.TypeForPrototype(proto)
	if err != nil {
		return fmt.Errorf("failed to get type for prototype %T: %w", proto, err)
	}

	r.internalResourceInputRepositoryPlugins[typ] = plugin

	if err := r.internalResourceInputRepositoryScheme.RegisterWithAlias(proto, typ); err != nil {
		return fmt.Errorf("failed to register type %T with alias %s: %w", proto, typ, err)
	}

	return nil
}

func RegisterInternalSourceInputPlugin(
	scheme *runtime.Scheme,
	r *RepositoryRegistry,
	plugin v1.ConstructionContract,
	proto runtime.Typed,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	typ, err := scheme.TypeForPrototype(proto)
	if err != nil {
		return fmt.Errorf("failed to get type for prototype %T: %w", proto, err)
	}

	r.internalSourceInputRepositoryPlugins[typ] = plugin

	if err := r.internalSourceInputRepositoryScheme.RegisterWithAlias(proto, typ); err != nil {
		return fmt.Errorf("failed to register type %T with alias %s: %w", proto, typ, err)
	}

	return nil
}

func RegisterInternalResourceDigestProcessorPlugin(
	scheme *runtime.Scheme,
	r *RepositoryRegistry,
	plugin v1.ConstructionContract,
	proto runtime.Typed,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	typ, err := scheme.TypeForPrototype(proto)
	if err != nil {
		return fmt.Errorf("failed to get type for prototype %T: %w", proto, err)
	}

	r.internalResourceDigestProcessorRepositoryPlugins[typ] = plugin

	if err := r.internalResourceDigestProcessorRepositoryScheme.RegisterWithAlias(proto, typ); err != nil {
		return fmt.Errorf("failed to register type %T with alias %s: %w", proto, typ, err)
	}

	return nil
}

type constructedPlugin struct {
	Plugin v1.ConstructionContract
	cmd    *exec.Cmd
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

func startAndReturnPlugin(ctx context.Context, r *RepositoryRegistry, plugin *mtypes.Plugin) (v1.ConstructionContract, error) {
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

	repoPlugin := NewConstructionRepositoryPlugin(client, plugin.ID, plugin.Path, plugin.Config, loc, jsonSchema)
	r.constructedPlugins[plugin.ID] = &constructedPlugin{
		Plugin: repoPlugin,
		cmd:    plugin.Cmd,
	}

	// wrap the untyped internal plugin into a typed representation.
	return repoPlugin, nil
}
