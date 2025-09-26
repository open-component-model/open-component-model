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
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// NewResourceRegistry creates a new registry and initializes maps.
func NewResourceRegistry(ctx context.Context) *ResourceRegistry {
	return &ResourceRegistry{
		ctx:                ctx,
		registry:           make(map[runtime.Type]types.Plugin),
		scheme:             runtime.NewScheme(runtime.WithAllowUnknown()),
		internalPlugins:    make(map[runtime.Type]repository.ResourceRepositoryProvider),
		constructedPlugins: make(map[string]*constructedPlugin),
	}
}

// ResourceRegistry holds all plugins that implement capabilities corresponding to RepositoryPlugin operations.
type ResourceRegistry struct {
	ctx                context.Context
	mu                 sync.Mutex
	registry           map[runtime.Type]types.Plugin
	constructedPlugins map[string]*constructedPlugin
	internalPlugins    map[runtime.Type]repository.ResourceRepositoryProvider
	scheme             *runtime.Scheme
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

func (r *ResourceRegistry) GetResourceRepositoryCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if this is an internal plugin first
	typ := repositorySpecification.GetType()
	if ok := r.scheme.IsRegistered(typ); ok {
		p, ok := r.internalPlugins[typ]
		if !ok {
			return nil, fmt.Errorf("no internal plugin registered for type %v", typ)
		}

		identity, err := p.GetResourceRepositoryCredentialConsumerIdentity(ctx, repositorySpecification)
		if err != nil {
			return nil, fmt.Errorf("failed to get component version repository: %w", err)
		}

		return identity, nil
	}

	// For external plugins, get the plugin and ask for identity
	plugin, err := r.getPlugin(ctx, typ)
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin for typ %q: %w", typ, err)
	}

	request := &v1.GetIdentityRequest[runtime.Typed]{
		Typ: repositorySpecification,
	}

	result, err := plugin.GetIdentity(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to get identity: %w", err)
	}

	return result.Identity, nil
}

func (r *ResourceRegistry) GetResourceRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (repository.ResourceRepository, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// look for an internal implementation that actually implements the interface
	_, _ = r.scheme.DefaultType(repositorySpecification)
	typ := repositorySpecification.GetType()
	// if we find the type has been registered internally, we look for internal plugins for it.
	if ok := r.scheme.IsRegistered(typ); ok {
		p, ok := r.internalPlugins[typ]
		if !ok {
			return nil, fmt.Errorf("no internal plugin registered for type %v", typ)
		}

		repo, err := p.GetResourceRepository(ctx, repositorySpecification, credentials)
		if err != nil {
			return nil, fmt.Errorf("failed to get component version repository: %w", err)
		}

		return repo, nil
	}

	plugin, err := r.getPlugin(ctx, typ)
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin for typ %q: %w", typ, err)
	}

	return r.externalToResourcePluginConverter(plugin, r.scheme, repositorySpecification), nil
}

// GetResourcePlugin returns Resource plugins for a specific type.
func (r *ResourceRegistry) GetResourcePlugin(ctx context.Context, spec runtime.Typed) (Repository, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// look for an internal implementation that actually implements the interface
	_, _ = r.scheme.DefaultType(spec)
	typ := spec.GetType()
	// if we find the type has been registered internally, we look for internal plugins for it.
	if ok := r.scheme.IsRegistered(typ); ok {
		p, ok := r.internalPlugins[typ]
		if !ok {
			return nil, fmt.Errorf("no internal plugin registered for type %v", typ)
		}

		return p, nil
	}

	plugin, err := r.getPlugin(ctx, typ)
	if err != nil {
		return nil, err
	}

	return r.externalToResourcePluginConverter(plugin, r.scheme), nil
}

// getPlugin returns a Resource plugin for a given type using a specific plugin storage map. It will also first look
// for existing registered internal plugins based on the type and the same registry name.
func (r *ResourceRegistry) getPlugin(ctx context.Context, spec runtime.Type) (v1.ReadWriteResourcePluginContract, error) {
	plugin, ok := r.registry[spec]
	if !ok {
		return nil, fmt.Errorf("failed to get plugin for typ %q", spec)
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
	for _, alias := range scheme.GetTypes()[typ] {
		r.internalPlugins[alias] = r.internalPlugins[typ]
	}

	if err := r.scheme.RegisterSchemeType(scheme, typ); err != nil {
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
