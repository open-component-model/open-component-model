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

	r.mu.Lock()
	defer r.mu.Unlock()

	_, _ = r.scheme.DefaultType(typed)
	typ := typed.GetType()
	if typ.IsEmpty() {
		return nil, fmt.Errorf("credential plugin lookup requires a type")
	}

	if internal, ok := r.internalPlugins[typ]; ok {
		return internal, nil
	}

	plugin, ok := r.registry[typ]
	if !ok {
		return nil, fmt.Errorf("no credential plugin registered for type %s", typ)
	}

	if existing, ok := r.constructedPlugins[plugin.ID]; ok {
		return NewCredentialPluginConverter(existing.Plugin), nil
	}

	external, err := startAndReturnPlugin(ctx, r, &plugin)
	if err != nil {
		return nil, err
	}
	return NewCredentialPluginConverter(external), nil
}

func startAndReturnPlugin(ctx context.Context, r *Registry, plugin *mtypes.Plugin) (credentialpluginv1.CredentialPluginContract[runtime.Typed], error) {
	if err := plugin.Cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start plugin: %s, %w", plugin.ID, err)
	}

	client, loc, err := plugins.WaitForPlugin(ctx, plugin)
	if err != nil {
		// Kill the orphaned subprocess to prevent resource leak
		_ = plugin.Cmd.Process.Kill()
		return nil, fmt.Errorf("failed to wait for plugin to start: %w", err)
	}

	// start log streaming once the plugin is up and running.
	// use the baseCtx here from the manager here so the streaming isn't stopped when the request is stopped.
	go plugins.StartLogStreamer(r.ctx, plugin)

	instance := NewCredentialPlugin(client, plugin.ID, plugin.Path, plugin.Config, loc, r.capabilities[plugin.ID])
	r.constructedPlugins[plugin.ID] = &constructedPlugin{
		Plugin: instance,
		cmd:    plugin.Cmd,
	}

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
