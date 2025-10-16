package componentlister

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts/componentlister/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// constructedPlugin only contains EXTERNAL plugins that have been started and need to be shut down.
type constructedPlugin struct {
	Plugin v1.ComponentListerPluginContract[runtime.Typed]
	cmd    *exec.Cmd
}

// RegisterInternalComponentListerPlugin is called to register an internal implementation for a component lister plugin.
func RegisterInternalComponentListerPlugin[T runtime.Typed](
	scheme *runtime.Scheme,
	r *ComponentListerRegistry,
	plugin InternalComponentListerPluginContract,
	proto T,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	typ, err := scheme.TypeForPrototype(proto)
	if err != nil {
		return fmt.Errorf("failed to get type for prototype %T: %w", proto, err)
	}

	r.internalComponentListerPlugins[typ] = plugin
	for _, alias := range scheme.GetTypes()[typ] {
		r.internalComponentListerPlugins[alias] = r.internalComponentListerPlugins[typ]
	}

	if err := r.scheme.RegisterSchemeType(scheme, typ); err != nil && !runtime.IsTypeAlreadyRegisteredError(err) {
		return fmt.Errorf("failed to register type %T with alias %s: %w", proto, typ, err)
	}

	return nil
}

// ComponentListerRegistry holds all plugins that implement capabilities corresponding to RepositoryPlugin operations.
type ComponentListerRegistry struct {
	ctx                            context.Context
	mu                             sync.Mutex
	registry                       map[runtime.Type]types.Plugin
	constructedPlugins             map[string]*constructedPlugin // running plugins
	internalComponentListerPlugins map[runtime.Type]InternalComponentListerPluginContract
	scheme                         *runtime.Scheme
}

// Shutdown will loop through all _STARTED_ plugins and will send an Interrupt signal to them.
// All plugins should handle interrupt signals gracefully. For Go, this is done automatically by
// the plugin SDK.
func (r *ComponentListerRegistry) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	eg, ctx := errgroup.WithContext(ctx)
	for _, p := range r.constructedPlugins {
		eg.Go(func() error {
			// The plugins should handle the Interrupt signal for shutdowns.
			if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
				return fmt.Errorf("failed to send interrupt signal to plugin: %w", errors.Join(err, p.cmd.Process.Kill()))
			}

			shutdownSig := make(chan error, 1)
			defer func() {
				close(shutdownSig)
			}()
			go func() {
				_, err := p.cmd.Process.Wait()
				shutdownSig <- err
			}()

			select {
			case err := <-shutdownSig:
				return err
			case <-ctx.Done():
				return errors.Join(ctx.Err(), p.cmd.Process.Kill())
			}
		})
	}

	return eg.Wait()
}

// AddPlugin takes a plugin discovered by the manager and puts it into the relevant internal map for
// tracking the plugin.
// This function will return an error if the given capability + type already has a registered plugin.
// Multiple plugins for the same cap+typ is not allowed.
func (r *ComponentListerRegistry) AddPlugin(plugin types.Plugin, constructionType runtime.Type) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if plugin, ok := r.registry[constructionType]; ok {
		return fmt.Errorf("plugin for construction type %q already registered with ID: %s", constructionType, plugin.ID)
	}

	// _Note_: No need to be more intricate because we know the endpoints, and we have a specific plugin here.
	r.registry[constructionType] = plugin

	return nil
}

func startAndReturnPlugin(ctx context.Context, r *ComponentListerRegistry, plugin *types.Plugin) (v1.ComponentListerPluginContract[runtime.Typed], error) {
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

	listerPlugin := NewComponentListerPlugin(client, plugin.ID, plugin.Path, plugin.Config, loc, jsonSchema)
	r.constructedPlugins[plugin.ID] = &constructedPlugin{
		Plugin: listerPlugin,
		cmd:    plugin.Cmd,
	}

	// wrap the untyped internal plugin into a typed representation.
	return listerPlugin, nil
}

// GetComponentListerCredentialConsumerIdentity retrieves the consumer identity
// for a component lister based on a given repository specification.
func (r *ComponentListerRegistry) GetComponentListerCredentialConsumerIdentity(ctx context.Context,
	repositorySpecification runtime.Typed,
) (runtime.Identity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if this is an internal plugin first
	typ := repositorySpecification.GetType()
	if ok := r.scheme.IsRegistered(typ); ok {
		p, ok := r.internalComponentListerPlugins[typ]
		if !ok {
			return nil, fmt.Errorf("no internal plugin registered for type %v", typ)
		}

		identity, err := p.GetComponentListerCredentialConsumerIdentity(ctx, repositorySpecification)
		if err != nil {
			return nil, fmt.Errorf("failed to get identity for the given specification '%+v': %w", repositorySpecification, err)
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

// GetComponentLister returns ComponentLister for a specific repository type.
func (r *ComponentListerRegistry) GetComponentLister(ctx context.Context,
	repositorySpecification runtime.Typed,
	credentials map[string]string,
) (repository.ComponentLister, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// look for an internal implementation that actually implements the interface
	_, _ = r.scheme.DefaultType(repositorySpecification)
	typ := repositorySpecification.GetType()
	// if we find the type has been registered internally, we look for internal plugins for it.
	if ok := r.scheme.IsRegistered(typ); ok {
		p, ok := r.internalComponentListerPlugins[typ]
		if !ok {
			return nil, fmt.Errorf("no internal plugin registered for type %v", typ)
		}

		lister, err := p.GetComponentLister(ctx, repositorySpecification, credentials)
		if err != nil {
			return nil, fmt.Errorf("failed to get component lister: %w", err)
		}

		return lister, nil
	}

	plugin, err := r.getPlugin(ctx, typ)
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin for typ %q: %w", typ, err)
	}

	// return this plugin wrapped with an ExternalPluginConverter
	return r.externalToComponentListerPluginConverter(plugin, r.scheme, repositorySpecification, credentials), nil
}

// getPlugin returns a Construction plugin for a given type using a specific plugin storage map. It will also first look
// for existing registered internal plugins based on the type and the same registry name.
func (r *ComponentListerRegistry) getPlugin(ctx context.Context, typ runtime.Type) (v1.ComponentListerPluginContract[runtime.Typed], error) {
	plugin, ok := r.registry[typ]
	if !ok {
		return nil, fmt.Errorf("failed to get plugin for typ %q", typ)
	}

	if existingPlugin, ok := r.constructedPlugins[plugin.ID]; ok {
		return existingPlugin.Plugin, nil
	}

	return startAndReturnPlugin(ctx, r, &plugin)
}

// NewComponentListerRegistry creates a new registry and initializes maps.
func NewComponentListerRegistry(ctx context.Context) *ComponentListerRegistry {
	return &ComponentListerRegistry{
		ctx: ctx,
		// Registry contains external plugins ONLY. Internal plugins that already have the implementation are in internalRepositoryPlugins.
		registry:                       make(map[runtime.Type]types.Plugin),
		constructedPlugins:             make(map[string]*constructedPlugin),
		scheme:                         runtime.NewScheme(runtime.WithAllowUnknown()),
		internalComponentListerPlugins: make(map[runtime.Type]InternalComponentListerPluginContract),
	}
}
