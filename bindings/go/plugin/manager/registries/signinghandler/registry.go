package signinghandler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"

	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/signing/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
)

// NewSigningRegistry creates a new registry and initializes maps.
func NewSigningRegistry(ctx context.Context) *SigningRegistry {
	return &SigningRegistry{
		ctx:                ctx,
		registry:           make(map[runtime.Type]types.Plugin),
		scheme:             runtime.NewScheme(runtime.WithAllowUnknown()),
		internalPlugins:    make(map[runtime.Type]signing.Handler),
		constructedPlugins: make(map[string]*constructedPlugin),
	}
}

// SigningRegistry holds all plugins that implement capabilities corresponding to SigningHandlerPlugin operations.
type SigningRegistry struct {
	ctx                context.Context
	mu                 sync.Mutex
	registry           map[runtime.Type]types.Plugin
	internalPlugins    map[runtime.Type]signing.Handler
	scheme             *runtime.Scheme
	constructedPlugins map[string]*constructedPlugin // running plugins
}

// ResourceScheme returns the scheme used by the Resource registry.
func (r *SigningRegistry) ResourceScheme() *runtime.Scheme {
	return r.scheme
}

// AddPlugin takes a plugin discovered by the manager and puts it into the relevant internal map for
// tracking the plugin.
func (r *SigningRegistry) AddPlugin(plugin types.Plugin, constructionType runtime.Type) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if plugin, ok := r.registry[constructionType]; ok {
		return fmt.Errorf("plugin for construction type %q already registered with ID: %s", constructionType, plugin.ID)
	}

	r.registry[constructionType] = plugin

	return nil
}

// GetPlugin returns plugins for a specific config spec type.
func (r *SigningRegistry) GetPlugin(ctx context.Context, spec runtime.Typed) (signing.Handler, error) {
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

	return r.externalPluginConverter(plugin, r.scheme), nil
}

// getPlugin returns a plugin for a given type using a specific plugin storage map. It will also first look
// for existing registered internal plugins based on the type and the same registry name.
func (r *SigningRegistry) getPlugin(ctx context.Context, spec runtime.Type) (v1.SignatureHandlerContract[runtime.Typed], error) {
	plugin, ok := r.registry[spec]
	if !ok {
		return nil, fmt.Errorf("failed to get plugin for typ %q", spec)
	}

	if existingPlugin, ok := r.constructedPlugins[plugin.ID]; ok {
		return existingPlugin.Plugin, nil
	}

	return startAndReturnPlugin(ctx, r, &plugin)
}

// RegisterInternalComponentSignatureHandler is called to register an internal implementation for a plugin.
func (r *SigningRegistry) RegisterInternalComponentSignatureHandler(
	plugin BuiltinSigningHandler,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for providerType, providerTypeAliases := range plugin.GetSigningHandlerScheme().GetTypes() {
		if err := r.scheme.RegisterSchemeType(plugin.GetSigningHandlerScheme(), providerType); err != nil {
			return fmt.Errorf("failed to register provider type %v: %w", providerType, err)
		}

		r.internalPlugins[providerType] = plugin
		for _, alias := range providerTypeAliases {
			r.internalPlugins[alias] = r.internalPlugins[providerType]
		}
	}

	return nil
}

type constructedPlugin struct {
	Plugin v1.SignatureHandlerContract[runtime.Typed]
	cmd    *exec.Cmd
}

// Shutdown will loop through all _STARTED_ plugins and will send an Interrupt signal to them.
// All plugins should handle interrupt signals gracefully. For Go, this is done automatically by
// the plugin SDK.
func (r *SigningRegistry) Shutdown(ctx context.Context) error {
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

func startAndReturnPlugin(ctx context.Context, r *SigningRegistry, plugin *types.Plugin) (v1.SignatureHandlerContract[runtime.Typed], error) {
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

	instance := NewSigningHandlerPlugin(client, plugin.ID, plugin.Path, plugin.Config, loc, jsonSchema)
	r.constructedPlugins[plugin.ID] = &constructedPlugin{
		Plugin: instance,
		cmd:    plugin.Cmd,
	}

	return instance, nil
}
