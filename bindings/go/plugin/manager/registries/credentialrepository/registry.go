package credentialrepository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentials/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// NewCredentialRepositoryRegistry creates a new registry and initializes maps.
func NewCredentialRepositoryRegistry(ctx context.Context) *RepositoryRegistry {
	return &RepositoryRegistry{
		ctx:                                 ctx,
		registry:                            make(map[runtime.Type]mtypes.Plugin),
		constructedPlugins:                  make(map[string]*constructedPlugin), // running plugins
		consumerTypeRegistrations:           make(map[runtime.Type]runtime.Type),
		internalCredentialRepositoryPlugins: make(map[runtime.Type]credentials.RepositoryPlugin),
		scheme:                              runtime.NewScheme(),
	}
}

// RepositoryRegistry holds all plugins that implement capabilities corresponding to RepositoryPlugin operations.
type RepositoryRegistry struct {
	ctx      context.Context
	mu       sync.Mutex
	registry map[runtime.Type]mtypes.Plugin
	scheme   *runtime.Scheme

	constructedPlugins        map[string]*constructedPlugin // running plugins
	consumerTypeRegistrations map[runtime.Type]runtime.Type
	// internalCredentialRepositoryPlugins contains all plugins that have been registered using internally import statement.
	internalCredentialRepositoryPlugins map[runtime.Type]credentials.RepositoryPlugin
}

func (r *RepositoryRegistry) RepositoryScheme() *runtime.Scheme {
	return r.scheme
}

// AddPlugin takes a credentialGraphPlugin discovered by the manager and adds it to the stored credentialGraphPlugin registry.
// This function will return an error if the given capability + type already has a registered credentialGraphPlugin.
// Multiple plugins for the same cap+typ is not allowed.
func (r *RepositoryRegistry) AddPlugin(plugin mtypes.Plugin, consumerIdentityType, configType runtime.Type) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if plugin, ok := r.registry[consumerIdentityType]; ok {
		return fmt.Errorf("credentialGraphPlugin for consumer identity type %q and config type %q already registered with ID: %s", consumerIdentityType, configType, plugin.ID)
	}

	// _Note_: No need to be more intricate because we know the endpoints, and we have a specific credentialGraphPlugin here.
	r.registry[configType] = plugin

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
func RegisterInternalCredentialRepositoryPlugin[T runtime.Typed](
	scheme *runtime.Scheme,
	r *RepositoryRegistry,
	plugin credentials.RepositoryPlugin,
	cfg T,
	consumerTypes []runtime.Type,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	typ, err := scheme.TypeForPrototype(cfg)
	if err != nil {
		return fmt.Errorf("failed to get type for prototype %T: %w", cfg, err)
	}

	r.internalCredentialRepositoryPlugins[typ] = plugin
	for _, alias := range scheme.GetTypes()[typ] {
		r.internalCredentialRepositoryPlugins[alias] = r.internalCredentialRepositoryPlugins[typ]
	}

	if err := r.scheme.RegisterSchemeType(scheme, typ); err != nil {
		return fmt.Errorf("failed to register type %T with alias %s: %w", cfg, typ, err)
	}

	for _, consumerType := range consumerTypes {
		r.consumerTypeRegistrations[consumerType] = typ
	}

	return nil
}

type constructedPlugin struct {
	Plugin v1.CredentialRepositoryPluginContract[runtime.Typed]
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

func startAndReturnPlugin(ctx context.Context, r *RepositoryRegistry, plugin *mtypes.Plugin) (v1.CredentialRepositoryPluginContract[runtime.Typed], error) {
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

	repoPlugin := NewCredentialRepositoryPlugin(client, plugin.ID, plugin.Path, plugin.Config, loc, jsonSchema)
	r.constructedPlugins[plugin.ID] = &constructedPlugin{
		Plugin: repoPlugin,
		cmd:    plugin.Cmd,
	}

	// wrap the untyped internal plugin into a typed representation.
	return repoPlugin, nil
}
