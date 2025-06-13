package resource

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"

	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/resource/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// NewResourceRegistry creates a new registry and initializes maps.
func NewResourceRegistry(ctx context.Context) *ResourceRegistry {
	return &ResourceRegistry{
		ctx:                ctx,
		registry:           make(map[runtime.Type]types.Plugin),
		resourceScheme:     runtime.NewScheme(runtime.WithAllowUnknown()),
		internalPlugins:    make(map[runtime.Type]Repository),
		constructedPlugins: make(map[string]*constructedPlugin),
	}
}

// ResourceRegistry holds all plugins that implement capabilities corresponding to RepositoryPlugin operations.
type ResourceRegistry struct {
	ctx                context.Context
	mu                 sync.Mutex
	registry           map[runtime.Type]types.Plugin
	internalPlugins    map[runtime.Type]Repository
	resourceScheme     *runtime.Scheme
	constructedPlugins map[string]*constructedPlugin // running plugins
}

// ResourceScheme returns the scheme used by the Resource registry.
func (r *ResourceRegistry) ResourceScheme() *runtime.Scheme {
	return r.resourceScheme
}

// AddPlugin takes a plugin discovered by the manager and puts it into the relevant internal map for
// tracking the plugin.
func (r *ResourceRegistry) AddPlugin(plugin types.Plugin, constructionType runtime.Type) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if plugin, ok := r.registry[constructionType]; ok {
		return fmt.Errorf("plugin for construction type %q already registered with ID: %s", constructionType, plugin.ID)
	}

	r.registry[constructionType] = plugin

	return nil
}

// GetResourcePlugin returns Resource plugins for a specific type.
func (r *ResourceRegistry) GetResourcePlugin(ctx context.Context, spec runtime.Typed) (Repository, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, err := r.resourceScheme.DefaultType(spec); err != nil {
		return nil, fmt.Errorf("failed to default type for prototype %T: %w", spec, err)
	}
	// if we find the type has been registered internally, we look for internal plugins for it.
	if typ, err := r.resourceScheme.TypeForPrototype(spec); err == nil {
		p, ok := r.internalPlugins[typ]
		if !ok {
			return nil, fmt.Errorf("no internal plugin registered for type %v", typ)
		}

		return p, nil
	}

	plugin, err := r.getPlugin(ctx, spec)
	if err != nil {
		return nil, err
	}

	return r.externalToResourcePluginConverter(plugin, r.resourceScheme), nil
}

// getPlugin returns a Resource plugin for a given type using a specific plugin storage map. It will also first look
// for existing registered internal plugins based on the type and the same registry name.
func (r *ResourceRegistry) getPlugin(ctx context.Context, spec runtime.Typed) (v1.ReadWriteResourcePluginContract, error) {
	// if we don't find the type registered internally, we look for external plugins by using the type
	// from the specification.
	typ := spec.GetType()
	if typ.IsEmpty() {
		return nil, fmt.Errorf("external plugins can not be fetched without a type %T", spec)
	}

	plugin, ok := r.registry[typ]
	if !ok {
		return nil, fmt.Errorf("failed to get plugin for typ %q", typ)
	}

	if existingPlugin, ok := r.constructedPlugins[plugin.ID]; ok {
		return existingPlugin.Plugin, nil
	}

	return startAndReturnPlugin(ctx, r, &plugin)
}

// RegisterInternalResourcePlugin is called to register an internal implementation for a resource plugin.
func RegisterInternalResourcePlugin(
	scheme *runtime.Scheme,
	r *ResourceRegistry,
	plugin Repository,
	proto runtime.Typed,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	typ, err := scheme.TypeForPrototype(proto)
	if err != nil {
		return fmt.Errorf("failed to get type for prototype %T: %w", proto, err)
	}

	r.internalPlugins[typ] = plugin

	if err := r.resourceScheme.RegisterWithAlias(proto, typ); err != nil {
		return fmt.Errorf("failed to register type %T with alias %s: %w", proto, typ, err)
	}

	return nil
}

type constructedPlugin struct {
	Plugin v1.ReadWriteResourcePluginContract
	cmd    *exec.Cmd
}

// Shutdown will loop through all _STARTED_ plugins and will send an Interrupt signal to them.
// All plugins should handle interrupt signals gracefully. For Go, this is done automatically by
// the plugin SDK.
func (r *ResourceRegistry) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var errs error
	for _, p := range r.constructedPlugins {
		// The plugins should handle the Interrupt signal for shutdowns.
		if perr := p.cmd.Process.Signal(os.Interrupt); perr != nil {
			errs = errors.Join(errs, perr)
		}
	}

	return errs
}

func startAndReturnPlugin(ctx context.Context, r *ResourceRegistry, plugin *types.Plugin) (v1.ReadWriteResourcePluginContract, error) {
	if err := plugin.Cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start plugin: %s, %w", plugin.ID, err)
	}

	client, loc, err := plugins.WaitForPlugin(ctx, plugin)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for plugin to start: %w", err)
	}

	// start log streaming once the plugin is up and running.
	go plugins.StartLogStreamer(r.ctx, plugin)

	var jsonSchema []byte
loop:
	for _, tps := range plugin.Types {
		for _, tp := range tps {
			jsonSchema = tp.JSONSchema
			break loop
		}
	}

	resourcePlugin := NewResourceRepositoryPlugin(client, plugin.ID, plugin.Path, plugin.Config, loc, jsonSchema)
	r.constructedPlugins[plugin.ID] = &constructedPlugin{
		Plugin: resourcePlugin,
		cmd:    plugin.Cmd,
	}

	return resourcePlugin, nil
}
