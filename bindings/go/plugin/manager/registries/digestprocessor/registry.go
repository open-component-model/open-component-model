package digestprocessor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"ocm.software/open-component-model/bindings/go/constructor"
	digestprocessorv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/digestprocessor/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type constructedPlugin struct {
	Plugin digestprocessorv1.ResourceDigestProcessorContract
	cmd    *exec.Cmd
}

// NewDigestProcessorRegistry creates a new registry and initializes maps.
func NewDigestProcessorRegistry(ctx context.Context) *RepositoryRegistry {
	return &RepositoryRegistry{
		ctx:                            ctx,
		capabilities:                   make(map[string]digestprocessorv1.CapabilitySpec),
		scheme:                         runtime.NewScheme(runtime.WithAllowUnknown()),
		registry:                       make(map[runtime.Type]mtypes.Plugin),
		constructedPlugins:             make(map[string]*constructedPlugin),
		internalDigestProcessorPlugins: make(map[runtime.Type]constructor.ResourceDigestProcessor),
	}
}

// RegisterInternalDigestProcessorPlugin can be called by actual implementations in the source.
// It will register any implementations directly for a given type and capability.
func (r *RepositoryRegistry) RegisterInternalDigestProcessorPlugin(
	plugin BuiltinDigestProcessorPlugin,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for providerType, providerTypeAliases := range plugin.GetResourceRepositoryScheme().GetTypes() {
		if err := r.scheme.RegisterSchemeType(plugin.GetResourceRepositoryScheme(), providerType); err != nil {
			return fmt.Errorf("failed to register provider type %v: %w", providerType, err)
		}

		r.internalDigestProcessorPlugins[providerType] = plugin
		for _, alias := range providerTypeAliases {
			r.internalDigestProcessorPlugins[alias] = r.internalDigestProcessorPlugins[providerType]
		}
	}

	return nil
}

// RepositoryRegistry holds all plugins that implement digest processor capabilities.
type RepositoryRegistry struct {
	ctx                            context.Context
	mu                             sync.Mutex
	scheme                         *runtime.Scheme
	capabilities                   map[string]digestprocessorv1.CapabilitySpec
	registry                       map[runtime.Type]mtypes.Plugin
	constructedPlugins             map[string]*constructedPlugin
	internalDigestProcessorPlugins map[runtime.Type]constructor.ResourceDigestProcessor
}

// Shutdown will loop through all _STARTED_ plugins and will send an Interrupt signal to them.
func (r *RepositoryRegistry) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var errs error
	for _, p := range r.constructedPlugins {
		if perr := p.cmd.Process.Signal(os.Interrupt); perr != nil && !errors.Is(perr, os.ErrProcessDone) {
			errs = errors.Join(errs, perr)
		}
	}
	return errs
}

// AddPlugin takes a plugin discovered by the manager and adds it to the stored plugin registry.
// This function will return an error if the given capability + type already has a registered plugin.
// Multiple plugins for the same cap+typ is not allowed.
func (r *RepositoryRegistry) AddPlugin(plugin mtypes.Plugin, spec runtime.Typed) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	capability := digestprocessorv1.CapabilitySpec{}
	if err := digestprocessorv1.Scheme.Convert(spec, &capability); err != nil {
		return fmt.Errorf("failed to convert object: %w", err)
	}
	if _, ok := r.capabilities[plugin.ID]; ok {
		return fmt.Errorf("plugin with ID %s already registered", plugin.ID)
	}
	r.capabilities[plugin.ID] = capability

	for _, typ := range capability.SupportedAccessTypes {
		if v, ok := r.registry[typ.Type]; ok {
			return fmt.Errorf("plugin for type %v already registered with ID: %s", typ.Type, v.ID)
		}
		// Note: No need to be more intricate because we know the endpoints, and we have a specific plugin here.
		r.registry[typ.Type] = plugin
	}

	return nil
}

func startAndReturnPlugin(ctx context.Context, r *RepositoryRegistry, plugin *mtypes.Plugin) (digestprocessorv1.ResourceDigestProcessorContract, error) {
	if err := plugin.Cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start plugin: %s, %w", plugin.ID, err)
	}

	client, loc, err := plugins.WaitForPlugin(ctx, plugin)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for plugin to start: %w", err)
	}

	go plugins.StartLogStreamer(r.ctx, plugin)

	digestPlugin := NewDigestProcessorPlugin(client, plugin.ID, plugin.Path, plugin.Config, loc, r.capabilities[plugin.ID])
	r.constructedPlugins[plugin.ID] = &constructedPlugin{
		Plugin: digestPlugin,
		cmd:    plugin.Cmd,
	}

	return digestPlugin, nil
}

func (r *RepositoryRegistry) GetPlugin(ctx context.Context, spec runtime.Typed) (constructor.ResourceDigestProcessor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// look for an internal implementation that actually implements the interface
	_, _ = r.scheme.DefaultType(spec)
	typ := spec.GetType()
	// if we find the type has been registered internally, we look for internal plugins for it.
	if ok := r.scheme.IsRegistered(typ); ok {
		p, ok := r.internalDigestProcessorPlugins[typ]
		if !ok {
			return nil, fmt.Errorf("no internal plugin registered for type %v", typ)
		}

		return p, nil
	}

	plugin, err := r.getPlugin(ctx, typ)
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin for typ %q: %w", typ, err)
	}

	return r.externalToResourceDigestProcessorPluginConverter(plugin, r.scheme), nil
}

func (r *RepositoryRegistry) getPlugin(ctx context.Context, typ runtime.Type) (digestprocessorv1.ResourceDigestProcessorContract, error) {
	plugin, ok := r.registry[typ]
	if !ok {
		return nil, fmt.Errorf("failed to get plugin for typ %q", typ)
	}

	if existingPlugin, ok := r.constructedPlugins[plugin.ID]; ok {
		return existingPlugin.Plugin, nil
	}

	return startAndReturnPlugin(ctx, r, &plugin)
}
