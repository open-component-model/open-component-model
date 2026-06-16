package credentialplugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"ocm.software/open-component-model/bindings/go/credentials"
	credentialpluginv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentialplugin/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ credentials.CredentialPluginProvider = (*Registry)(nil)

// Registry holds registered credential plugins and resolves them by type.
type Registry struct {
	ctx             context.Context
	mu              sync.Mutex
	capabilities    map[string]credentialpluginv1.CapabilitySpec
	registry        map[runtime.Type]mtypes.Plugin
	scheme          *runtime.Scheme
	internalPlugins map[runtime.Type]credentials.CredentialPlugin

	constructedPlugins map[string]*constructedPlugin
}

type constructedPlugin struct {
	Plugin credentialpluginv1.CredentialPluginContract[runtime.Typed]
	cmd    *exec.Cmd
}

// NewRegistry creates a new credential plugin registry.
func NewRegistry(ctx context.Context) *Registry {
	return &Registry{
		ctx:                ctx,
		capabilities:       make(map[string]credentialpluginv1.CapabilitySpec),
		registry:           make(map[runtime.Type]mtypes.Plugin),
		scheme:             runtime.NewScheme(),
		internalPlugins:    make(map[runtime.Type]credentials.CredentialPlugin),
		constructedPlugins: make(map[string]*constructedPlugin),
	}
}

// AddPlugin takes a plugin discovered by the manager and adds it to the stored plugin registry.
// This function will return an error if the given capability + type already has a registered plugin.
func (r *Registry) AddPlugin(plugin mtypes.Plugin, spec runtime.Typed) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	capability := credentialpluginv1.CapabilitySpec{}
	if err := credentialpluginv1.Scheme.Convert(spec, &capability); err != nil {
		return fmt.Errorf("failed to convert object: %w", err)
	}
	if _, ok := r.capabilities[plugin.ID]; ok {
		return fmt.Errorf("plugin with ID %s already registered", plugin.ID)
	}
	r.capabilities[plugin.ID] = capability

	for _, typ := range capability.SupportedCredentialPluginTypes {
		if v, ok := r.registry[typ.Type]; ok {
			return fmt.Errorf("plugin for type %v already registered with ID: %s", typ.Type, v.ID)
		}
		r.registry[typ.Type] = plugin
	}

	return nil
}

// RegisterInternalCredentialPlugin registers a builtin credential plugin for
// all types declared in its scheme.
func (r *Registry) RegisterInternalCredentialPlugin(plugin BuiltinCredentialPlugin) error {
	if plugin == nil {
		return fmt.Errorf("cannot register nil credential plugin")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for providerType, providerTypeAliases := range plugin.GetCredentialPluginScheme().GetTypes() {
		if err := r.scheme.RegisterSchemeType(plugin.GetCredentialPluginScheme(), providerType); err != nil {
			return fmt.Errorf("failed to register provider type %v: %w", providerType, err)
		}

		r.internalPlugins[providerType] = plugin
		for _, alias := range providerTypeAliases {
			r.internalPlugins[alias] = plugin
		}
	}

	return nil
}

// GetCredentialPlugin returns the credential plugin for the given typed spec.
func (r *Registry) GetCredentialPlugin(ctx context.Context, typed runtime.Typed) (credentials.CredentialPlugin, error) {
	if typed == nil {
		return nil, fmt.Errorf("credential plugin lookup requires a non-nil typed argument")
	}

	res, err := r.lookup(typed)
	if err != nil {
		return nil, err
	}
	if res.internal != nil {
		return res.internal, nil
	}
	if res.cached != nil {
		return NewCredentialPluginConverter(res.cached), nil
	}

	// Start the subprocess without holding the registry lock so concurrent
	// lookups for other types proceed and Shutdown is not blocked on plugin boot.
	externalPlugin, err := r.startAndReturnPlugin(ctx, &res.plugin)
	if err != nil {
		return nil, err
	}
	return NewCredentialPluginConverter(externalPlugin), nil
}

// lookupResult carries the outcome of a registry lookup. Exactly one of
// internal, cached, or plugin is set on success: internal for a builtin
// plugin, cached for an already-started external plugin, or plugin for a
// discovered external plugin descriptor that the caller must start.
type lookupResult struct {
	internal credentials.CredentialPlugin
	cached   credentialpluginv1.CredentialPluginContract[runtime.Typed]
	plugin   mtypes.Plugin
}

func (r *Registry) lookup(typed runtime.Typed) (lookupResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// DefaultType resolves short-type aliases registered via the internal
	// scheme (see RegisterInternalCredentialPlugin). External plugins arrive
	// as *runtime.Raw with the type already set, which is not a registered
	// prototype and is rejected here — the subsequent map lookup uses the
	// raw GetType() value, so the failure is benign.
	_, _ = r.scheme.DefaultType(typed)
	typ := typed.GetType()
	if typ.IsEmpty() {
		return lookupResult{}, fmt.Errorf("credential plugin lookup requires a type")
	}

	if internal, ok := r.internalPlugins[typ]; ok {
		return lookupResult{internal: internal}, nil
	}

	plugin, ok := r.registry[typ]
	if !ok {
		return lookupResult{}, fmt.Errorf("no credential plugin registered for type %s", typ)
	}
	if existing, ok := r.constructedPlugins[plugin.ID]; ok {
		return lookupResult{cached: existing.Plugin}, nil
	}
	return lookupResult{plugin: plugin}, nil
}

// startAndReturnPlugin starts the subprocess, waits for it to be ready, and
// registers it in constructedPlugins. If a concurrent caller already won the
// race and registered a plugin for the same ID, the freshly started subprocess
// is interrupted and the existing instance is returned instead.
func (r *Registry) startAndReturnPlugin(ctx context.Context, plugin *mtypes.Plugin) (credentialpluginv1.CredentialPluginContract[runtime.Typed], error) {
	if err := plugin.Cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start plugin: %s, %w", plugin.ID, err)
	}

	client, loc, err := plugins.WaitForPlugin(ctx, plugin)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for plugin to start: %w", err)
	}

	r.mu.Lock()
	if existing, ok := r.constructedPlugins[plugin.ID]; ok {
		r.mu.Unlock()
		if perr := plugin.Cmd.Process.Signal(os.Interrupt); perr != nil && !errors.Is(perr, os.ErrProcessDone) {
			return nil, fmt.Errorf("failed to interrupt duplicate plugin start for %s: %w", plugin.ID, perr)
		}
		return existing.Plugin, nil
	}
	instance := NewCredentialPlugin(client, plugin.ID, plugin.Path, plugin.Config, loc, r.capabilities[plugin.ID])
	r.constructedPlugins[plugin.ID] = &constructedPlugin{
		Plugin: instance,
		cmd:    plugin.Cmd,
	}
	r.mu.Unlock()

	go plugins.StartLogStreamer(r.ctx, plugin)

	return instance, nil
}

// Shutdown will loop through all _STARTED_ plugins and will send an Interrupt signal to them.
func (r *Registry) Shutdown(ctx context.Context) error {
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

// Scheme returns the registry's type scheme.
func (r *Registry) Scheme() *runtime.Scheme {
	return r.scheme
}
