package componentversionrepository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type constructedPlugin struct {
	Plugin v1.ReadWriteOCMRepositoryPluginContract[runtime.Typed]

	cmd *exec.Cmd
}

// RegisterInternalComponentVersionRepositoryPlugin can be called by actual implementations in the source.
// It will register any implementations directly for a given type and capability.
func RegisterInternalComponentVersionRepositoryPlugin[T runtime.Typed](
	scheme *runtime.Scheme,
	r *RepositoryRegistry,
	p repository.ComponentVersionRepositoryProvider,
	prototype T,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	typ, err := scheme.TypeForPrototype(prototype)
	if err != nil {
		return fmt.Errorf("failed to get type for prototype %T: %w", prototype, err)
	}

	r.internalComponentVersionRepositoryPlugins[typ] = p
	for _, alias := range scheme.GetTypes()[typ] {
		r.internalComponentVersionRepositoryPlugins[alias] = r.internalComponentVersionRepositoryPlugins[typ]
	}

	if err := r.scheme.RegisterSchemeType(scheme, typ); err != nil {
		return fmt.Errorf("failed to register prototype %T: %w", prototype, err)
	}

	return nil
}

// RepositoryRegistry holds all plugins that implement capabilities corresponding to RepositoryPlugin operations.
// It implements the ComponentVersionRepositoryProvider interface.
type RepositoryRegistry struct {
	ctx                context.Context
	mu                 sync.RWMutex
	registry           map[runtime.Type]mtypes.Plugin // Have this as a single plugin for read/write
	constructedPlugins map[string]*constructedPlugin  // running plugins

	// internalComponentVersionRepositoryPlugins contains all plugins that have been registered using internally import statement.
	internalComponentVersionRepositoryPlugins map[runtime.Type]repository.ComponentVersionRepositoryProvider
	// scheme is the holder of schemes. This hold will contain the scheme required to
	// construct and understand the passed in types and what / how they need to look like. The passed in scheme during
	// registration will be added to this scheme holder. Once this happens, the code will validate any passed in objects
	// that their type is registered or not.
	scheme *runtime.Scheme
}

// Ensure RepositoryRegistry implements ComponentVersionRepositoryProvider interface
var _ repository.ComponentVersionRepositoryProvider = (*RepositoryRegistry)(nil)

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

func startAndReturnPlugin(ctx context.Context, r *RepositoryRegistry, plugin *mtypes.Plugin) (v1.ReadWriteOCMRepositoryPluginContract[runtime.Typed], error) {
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
	return repoPlugin, nil
}

// GetComponentVersionRepositoryCredentialConsumerIdentity retrieves the consumer identity
// for a component version repository based on a given repository specification.
func (r *RepositoryRegistry) GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Check if this is an internal plugin first
	typ := repositorySpecification.GetType()
	if ok := r.scheme.IsRegistered(typ); ok {
		p, ok := r.internalComponentVersionRepositoryPlugins[typ]
		if !ok {
			return nil, fmt.Errorf("no internal plugin registered for type %v", typ)
		}

		identity, err := p.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, repositorySpecification)
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

// GetComponentVersionRepository retrieves a component version repository based on a given
// repository specification and credentials.
// It first checks for internal plugins registered via RegisterInternalComponentVersionRepositoryPlugin,
// then falls back to external plugins if no internal plugin is found.
func (r *RepositoryRegistry) GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (repository.ComponentVersionRepository, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// look for an internal implementation that actually implements the interface
	_, _ = r.scheme.DefaultType(repositorySpecification)
	typ := repositorySpecification.GetType()
	// if we find the type has been registered internally, we look for internal plugins for it.
	if ok := r.scheme.IsRegistered(typ); ok {
		p, ok := r.internalComponentVersionRepositoryPlugins[typ]
		if !ok {
			return nil, fmt.Errorf("no internal plugin registered for type %v", typ)
		}

		repo, err := p.GetComponentVersionRepository(ctx, repositorySpecification, credentials)
		if err != nil {
			return nil, fmt.Errorf("failed to get component version repository: %w", err)
		}

		return repo, nil
	}

	plugin, err := r.getPlugin(ctx, typ)
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin for typ %q: %w", typ, err)
	}

	return r.externalToComponentVersionRepository(plugin, r.scheme, repositorySpecification, credentials), nil
}

func (r *RepositoryRegistry) getPlugin(ctx context.Context, typ runtime.Type) (v1.ReadWriteOCMRepositoryPluginContract[runtime.Typed], error) {
	plugin, ok := r.registry[typ]
	if !ok {
		return nil, fmt.Errorf("failed to get plugin for typ %q", typ)
	}

	if existingPlugin, ok := r.constructedPlugins[plugin.ID]; ok {
		return existingPlugin.Plugin, nil
	}

	return startAndReturnPlugin(ctx, r, &plugin)
}

// NewComponentVersionRepositoryRegistry creates a new registry and initializes maps.
func NewComponentVersionRepositoryRegistry(ctx context.Context) *RepositoryRegistry {
	return &RepositoryRegistry{
		ctx:                ctx,
		registry:           make(map[runtime.Type]mtypes.Plugin),
		constructedPlugins: make(map[string]*constructedPlugin),
		scheme:             runtime.NewScheme(runtime.WithAllowUnknown()),
		internalComponentVersionRepositoryPlugins: make(map[runtime.Type]repository.ComponentVersionRepositoryProvider),
	}
}
