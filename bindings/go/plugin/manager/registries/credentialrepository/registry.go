package credentialrepository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"ocm.software/open-component-model/bindings/go/credentials"
	credentialsv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentials/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// NewCredentialRepositoryRegistry creates a new registry and initializes maps.
func NewCredentialRepositoryRegistry(ctx context.Context) *RepositoryRegistry {
	return &RepositoryRegistry{
		ctx:                                 ctx,
		capabilities:                        make(map[string]credentialsv1.CapabilitySpec),
		registry:                            make(map[runtime.Type]mtypes.Plugin),
		constructedPlugins:                  make(map[string]*constructedPlugin), // running plugins
		consumerTypeRegistrations:           make(map[runtime.Type]runtime.Type),
		internalCredentialRepositoryPlugins: make(map[runtime.Type]credentials.RepositoryPlugin),
		scheme:                              runtime.NewScheme(),
	}
}

// RepositoryRegistry holds all plugins that implement capabilities corresponding to RepositoryPlugin operations.
type RepositoryRegistry struct {
	ctx          context.Context
	mu           sync.Mutex
	capabilities map[string]credentialsv1.CapabilitySpec
	registry     map[runtime.Type]mtypes.Plugin
	scheme       *runtime.Scheme

	constructedPlugins        map[string]*constructedPlugin // running plugins
	consumerTypeRegistrations map[runtime.Type]runtime.Type
	// internalCredentialRepositoryPlugins contains all plugins that have been registered using internally import statement.
	internalCredentialRepositoryPlugins map[runtime.Type]credentials.RepositoryPlugin
}

func (r *RepositoryRegistry) RepositoryScheme() *runtime.Scheme {
	return r.scheme
}

// AddPlugin takes a plugin discovered by the manager and adds it to the stored plugin registry.
// This function will return an error if the given capability + type already has a registered plugin.
// Multiple plugins for the same cap+typ is not allowed.
func (r *RepositoryRegistry) AddPlugin(plugin mtypes.Plugin, spec runtime.Typed) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	capability := credentialsv1.CapabilitySpec{}
	if err := credentialsv1.Scheme.Convert(spec, &capability); err != nil {
		return fmt.Errorf("failed to convert object: %w", err)
	}
	if _, ok := r.capabilities[plugin.ID]; ok {
		return fmt.Errorf("plugin with ID %s already registered", plugin.ID)
	}
	r.capabilities[plugin.ID] = capability

	for _, typ := range capability.SupportedConsumerIdentityTypes {
		if v, ok := r.registry[typ.Type]; ok {
			return fmt.Errorf("plugin for type %v already registered with ID: %s", typ.Type, v.ID)
		}
		// Note: No need to be more intricate because we know the endpoints, and we have a specific plugin here.
		r.registry[typ.Type] = plugin
	}

	return nil
}

func (r *RepositoryRegistry) GetPlugin(ctx context.Context, spec runtime.Typed) (credentials.RepositoryPlugin, error) {
	if _, err := r.scheme.DefaultType(spec); err != nil {
		return nil, fmt.Errorf("failed to default type for prototype %T: %w", spec, err)
	}
	// if we find the type has been registered internally, we look for internal plugins for it.
	if typ, err := r.scheme.TypeForPrototype(spec); err == nil {
		p, ok := r.internalCredentialRepositoryPlugins[typ]
		if !ok {
			return nil, fmt.Errorf("no internal plugin registered for type %v", typ)
		}
		return p, nil
	}

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
		// Convert the external plugin to internal interface using the converter
		return NewCredentialRepositoryPluginConverter(existingPlugin.Plugin), nil
	}

	externalPlugin, err := startAndReturnPlugin(ctx, r, &plugin)
	if err != nil {
		return nil, err
	}
	// Convert the external plugin to internal interface using the converter
	return NewCredentialRepositoryPluginConverter(externalPlugin), nil
}

// RegisterInternalCredentialRepositoryPlugin can be called by actual implementations in the source.
// It will register any implementations directly for a given type and capability.
func (r *RepositoryRegistry) RegisterInternalCredentialRepositoryPlugin(
	plugin BuiltinCredentialRepositoryPlugin,
	consumerTypes []runtime.Type,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for providerType, providerTypeAliases := range plugin.GetCredentialRepositoryScheme().GetTypes() {
		if err := r.scheme.RegisterSchemeType(plugin.GetCredentialRepositoryScheme(), providerType); err != nil {
			return fmt.Errorf("failed to register provider type %v: %w", providerType, err)
		}

		r.internalCredentialRepositoryPlugins[providerType] = plugin
		for _, alias := range providerTypeAliases {
			r.internalCredentialRepositoryPlugins[alias] = r.internalCredentialRepositoryPlugins[providerType]
		}

		for _, consumerType := range consumerTypes {
			r.consumerTypeRegistrations[consumerType] = providerType
		}
	}

	return nil
}

type constructedPlugin struct {
	Plugin credentialsv1.CredentialRepositoryPluginContract[runtime.Typed]
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
		if perr := p.cmd.Process.Signal(os.Interrupt); perr != nil && !errors.Is(perr, os.ErrProcessDone) {
			errs = errors.Join(errs, perr)
		}
	}

	return errs
}

func startAndReturnPlugin(ctx context.Context, r *RepositoryRegistry, plugin *mtypes.Plugin) (credentialsv1.CredentialRepositoryPluginContract[runtime.Typed], error) {
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

	repoPlugin := NewCredentialRepositoryPlugin(client, plugin.ID, plugin.Path, plugin.Config, loc, r.capabilities[plugin.ID])
	r.constructedPlugins[plugin.ID] = &constructedPlugin{
		Plugin: repoPlugin,
		cmd:    plugin.Cmd,
	}

	// wrap the untyped internal plugin into a typed representation.
	return repoPlugin, nil
}
