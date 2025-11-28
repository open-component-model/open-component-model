package blobtransformer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"ocm.software/open-component-model/bindings/go/blob/transformer"
	blobtransformerv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/blobtransformer/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type constructedPlugin struct {
	Plugin blobtransformerv1.BlobTransformerPluginContract[runtime.Typed]

	cmd *exec.Cmd
}

// Registry holds all plugins that implement capabilities corresponding to RepositoryPlugin operations.
type Registry struct {
	ctx                context.Context
	mu                 sync.Mutex
	capabilities       map[string]blobtransformerv1.CapabilitySpec
	registry           map[runtime.Type]mtypes.Plugin // Have this as a single plugin for read/write
	constructedPlugins map[string]*constructedPlugin  // running plugins

	// internal contains all plugins that have been registered using internally import statement.
	internal map[runtime.Type]transformer.Transformer
	// scheme is the holder of schemes. This hold will contain the scheme required to
	// construct and understand the passed in types and what / how they need to look like. The passed in scheme during
	// registration will be added to this scheme holder. Once this happens, the code will validate any passed in objects
	// that their type is registered or not.
	scheme *runtime.Scheme
}

// RegisterInternalBlobTransformerPlugin can be called by actual implementations in the source.
// It will register any implementations directly for a given type and capability.
func (r *Registry) RegisterInternalBlobTransformerPlugin(
	plugin BuiltinBlobTransformer,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for providerType, providerTypeAliases := range plugin.GetTransformerScheme().GetTypes() {
		if err := r.scheme.RegisterSchemeType(plugin.GetTransformerScheme(), providerType); err != nil {
			return fmt.Errorf("failed to register provider type %v: %w", providerType, err)
		}

		r.internal[providerType] = plugin
		for _, alias := range providerTypeAliases {
			r.internal[alias] = r.internal[providerType]
		}
	}

	return nil
}

// Shutdown will loop through all _STARTED_ plugins and will send an Interrupt signal to them.
// All plugins should handle interrupt signals gracefully. For Go, this is done automatically by
// the plugin SDK.
func (r *Registry) Shutdown(ctx context.Context) error {
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
func (r *Registry) AddPlugin(plugin mtypes.Plugin, spec runtime.Typed) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	capability := blobtransformerv1.CapabilitySpec{}
	if err := blobtransformerv1.Scheme.Convert(spec, &capability); err != nil {
		return fmt.Errorf("failed to convert object: %w", err)
	}
	if _, ok := r.capabilities[plugin.ID]; ok {
		return fmt.Errorf("plugin with ID %s already registered", plugin.ID)
	}
	r.capabilities[plugin.ID] = capability

	for _, typ := range capability.SupportedTransformerSpecTypes {
		if v, ok := r.registry[typ.Type]; ok {
			return fmt.Errorf("plugin for type %v already registered with ID: %s", typ.Type, v.ID)
		}
		// Note: No need to be more intricate because we know the endpoints, and we have a specific plugin here.
		r.registry[typ.Type] = plugin
	}

	return nil
}

func startAndReturnPlugin(ctx context.Context, r *Registry, plugin *mtypes.Plugin) (blobtransformerv1.BlobTransformerPluginContract[runtime.Typed], error) {
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

	repoPlugin := NewPlugin(client, plugin.ID, plugin.Path, plugin.Config, loc, r.capabilities[plugin.ID])
	r.constructedPlugins[plugin.ID] = &constructedPlugin{
		Plugin: repoPlugin,
		cmd:    plugin.Cmd,
	}

	// wrap the untyped internal plugin into a typed representation.
	return repoPlugin, nil
}

// GetPlugin retrieves a plugin for the given specification type.
// It first checks for internal plugins registered via RegisterInternalComponentVersionRepositoryPlugin,
// then falls back to external plugins if no internal plugin is found.
func (r *Registry) GetPlugin(ctx context.Context, spec runtime.Typed) (transformer.Transformer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, _ = r.scheme.DefaultType(spec)
	typ := spec.GetType()
	// if we find the type has been registered internally, we look for internal plugins for it.
	if ok := r.scheme.IsRegistered(typ); ok {
		p, ok := r.internal[typ]
		if !ok {
			return nil, fmt.Errorf("no internal plugin registered for type %v", typ)
		}

		return p, nil
	}

	plugin, err := r.getPlugin(ctx, typ)
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin: %w", err)
	}

	return r.externalToBlobTransformerConverter(plugin, r.scheme), nil
}

func (r *Registry) getPlugin(ctx context.Context, typ runtime.Type) (blobtransformerv1.BlobTransformerPluginContract[runtime.Typed], error) {
	plugin, ok := r.registry[typ]
	if !ok {
		return nil, fmt.Errorf("failed to get plugin for typ %q", typ)
	}

	if existingPlugin, ok := r.constructedPlugins[plugin.ID]; ok {
		return existingPlugin.Plugin, nil
	}

	return startAndReturnPlugin(ctx, r, &plugin)
}

// NewBlobTransformerRegistry creates a new registry and initializes maps.
func NewBlobTransformerRegistry(ctx context.Context) *Registry {
	return &Registry{
		ctx:                ctx,
		capabilities:       make(map[string]blobtransformerv1.CapabilitySpec),
		registry:           make(map[runtime.Type]mtypes.Plugin),
		constructedPlugins: make(map[string]*constructedPlugin),
		scheme:             runtime.NewScheme(runtime.WithAllowUnknown()),
		internal:           make(map[runtime.Type]transformer.Transformer),
	}
}
