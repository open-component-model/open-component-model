package input

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"

	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/constructor"
	inputv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/input/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// NewInputRepositoryRegistry creates a new registry and initializes maps.
func NewInputRepositoryRegistry(ctx context.Context) *RepositoryRegistry {
	return &RepositoryRegistry{
		ctx:          ctx,
		capabilities: make(map[string]inputv1.CapabilitySpec),
		// Registry contains external plugins ONLY. Internal plugins that already have the implementation are in internalRepositoryPlugins.
		registry:                               make(map[runtime.Type]types.Plugin),
		scheme:                                 runtime.NewScheme(runtime.WithAllowUnknown()),
		internalResourceInputRepositoryPlugins: make(map[runtime.Type]constructor.ResourceInputMethod),
		internalSourceInputRepositoryPlugins:   make(map[runtime.Type]constructor.SourceInputMethod),
		constructedPlugins:                     make(map[string]*constructedPlugin),
	}
}

// RepositoryRegistry holds all plugins that implement capabilities corresponding to RepositoryPlugin operations.
type RepositoryRegistry struct {
	ctx                                    context.Context
	mu                                     sync.Mutex
	capabilities                           map[string]inputv1.CapabilitySpec
	registry                               map[runtime.Type]types.Plugin
	scheme                                 *runtime.Scheme
	internalResourceInputRepositoryPlugins map[runtime.Type]constructor.ResourceInputMethod
	internalSourceInputRepositoryPlugins   map[runtime.Type]constructor.SourceInputMethod
	constructedPlugins                     map[string]*constructedPlugin // running plugins
}

// InputRepositoryScheme returns the scheme used by the ResourceInput registry.
func (r *RepositoryRegistry) InputRepositoryScheme() *runtime.Scheme {
	return r.scheme
}

// AddPlugin takes a plugin discovered by the manager and adds it to the stored plugin registry.
// This function will return an error if the given capability + type already has a registered plugin.
// Multiple plugins for the same cap+typ is not allowed.
func (r *RepositoryRegistry) AddPlugin(plugin types.Plugin, spec runtime.Typed) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	capability := inputv1.CapabilitySpec{}
	if err := inputv1.Scheme.Convert(spec, &capability); err != nil {
		return fmt.Errorf("failed to convert object: %w", err)
	}
	r.capabilities[plugin.ID] = capability

	for _, typ := range capability.SupportedInputTypes {
		if v, ok := r.registry[typ.Type]; ok {
			return fmt.Errorf("plugin for type %v already registered with ID: %s", typ.Type, v.ID)
		}
		// Note: No need to be more intricate because we know the endpoints, and we have a specific plugin here.
		r.registry[typ.Type] = plugin
	}

	return nil
}

// GetResourceInputPlugin returns ResourceInput plugins for a specific type.
func (r *RepositoryRegistry) GetResourceInputPlugin(ctx context.Context, spec runtime.Typed) (constructor.ResourceInputMethod, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// look for an internal implementation that actually implements the interface
	_, _ = r.scheme.DefaultType(spec)
	typ := spec.GetType()
	// if we find the type has been registered internally, we look for internal plugins for it.
	if ok := r.scheme.IsRegistered(typ); ok {
		p, ok := r.internalResourceInputRepositoryPlugins[typ]
		if !ok {
			return nil, fmt.Errorf("no internal plugin registered for type %v", typ)
		}

		return p, nil
	}

	plugin, err := r.getPlugin(ctx, spec)
	if err != nil {
		return nil, err
	}

	// return this plugin wrapped with an ExternalPluginConverter
	return r.externalToResourceInputPluginConverter(plugin, r.scheme), nil
}

// GetSourceInputPlugin returns SourceInput plugins for a specific type.
func (r *RepositoryRegistry) GetSourceInputPlugin(ctx context.Context, spec runtime.Typed) (constructor.SourceInputMethod, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// look for an internal implementation that actually implements the interface
	_, _ = r.scheme.DefaultType(spec)
	typ := spec.GetType()
	// if we find the type has been registered internally, we look for internal plugins for it.
	if ok := r.scheme.IsRegistered(typ); ok {
		p, ok := r.internalSourceInputRepositoryPlugins[typ]
		if !ok {
			return nil, fmt.Errorf("no internal plugin registered for type %v", typ)
		}

		return p, nil
	}

	plugin, err := r.getPlugin(ctx, spec)
	if err != nil {
		return nil, err
	}

	return r.externalToSourceInputPluginConverter(plugin, r.scheme), nil
}

// getPlugin returns a Construction plugin for a given type using a specific plugin storage map. It will also first look
// for existing registered internal plugins based on the type and the same registry name.
func (r *RepositoryRegistry) getPlugin(ctx context.Context, spec runtime.Typed) (inputv1.InputPluginContract, error) {
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
		if existingPlugin.wasmPlugin != nil {
			return existingPlugin.wasmPlugin, nil
		}

		return existingPlugin.Plugin, nil
	}

	return startAndReturnPlugin(ctx, r, &plugin)
}

// RegisterInternalResourceInputPlugin is called to register an internal implementation for an input plugin.
func (r *RepositoryRegistry) RegisterInternalResourceInputPlugin(
	plugin BuiltinResourceInputMethod,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for providerType, providerTypeAliases := range plugin.GetInputMethodScheme().GetTypes() {
		if !r.scheme.IsRegistered(providerType) {
			if err := r.scheme.RegisterSchemeType(plugin.GetInputMethodScheme(), providerType); err != nil {
				return fmt.Errorf("failed to register provider type %v: %w", providerType, err)
			}
		} else {
			aliases := r.scheme.GetTypes()[providerType]
			for _, alias := range providerTypeAliases {
				if slices.Contains(aliases, alias) {
					continue
				}
				return fmt.Errorf("provider type %q already registered with different aliases: %s", providerType, alias)
			}
			registeredObj, err := r.scheme.NewObject(providerType)
			if err != nil {
				return fmt.Errorf("failed to create new object for type %v: %w", providerType, err)
			}
			newObject, err := plugin.GetInputMethodScheme().NewObject(providerType)
			if err != nil {
				return fmt.Errorf("failed to create new object for type %v: %w", providerType, err)
			}
			if err := r.scheme.Convert(newObject, registeredObj); err != nil {
				return fmt.Errorf("provider type %q already registered with different object type, expected %T, got %T: %w", providerType, registeredObj, newObject, err)
			}
		}

		r.internalResourceInputRepositoryPlugins[providerType] = plugin
		for _, alias := range providerTypeAliases {
			r.internalResourceInputRepositoryPlugins[alias] = r.internalResourceInputRepositoryPlugins[providerType]
		}
	}

	return nil
}

// RegisterInternalSourceInputPlugin is called to register an internal implementation for an input plugin.
func (r *RepositoryRegistry) RegisterInternalSourceInputPlugin(
	plugin BuiltinSourceInputMethod,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for providerType, providerTypeAliases := range plugin.GetInputMethodScheme().GetTypes() {
		if !r.scheme.IsRegistered(providerType) {
			if err := r.scheme.RegisterSchemeType(plugin.GetInputMethodScheme(), providerType); err != nil {
				return fmt.Errorf("failed to register provider type %v: %w", providerType, err)
			}
		} else {
			aliases := r.scheme.GetTypes()[providerType]
			for _, alias := range providerTypeAliases {
				if slices.Contains(aliases, alias) {
					continue
				}
				return fmt.Errorf("provider type %q already registered with different aliases: %s", providerType, alias)
			}
			registeredObj, err := r.scheme.NewObject(providerType)
			if err != nil {
				return fmt.Errorf("failed to create new object for type %v: %w", providerType, err)
			}
			newObject, err := plugin.GetInputMethodScheme().NewObject(providerType)
			if err != nil {
				return fmt.Errorf("failed to create new object for type %v: %w", providerType, err)
			}
			if err := r.scheme.Convert(newObject, registeredObj); err != nil {
				return fmt.Errorf("provider type %q already registered with different object type, expected %T, got %T: %w", providerType, registeredObj, newObject, err)
			}
		}

		r.internalSourceInputRepositoryPlugins[providerType] = plugin
		for _, alias := range providerTypeAliases {
			r.internalSourceInputRepositoryPlugins[alias] = r.internalSourceInputRepositoryPlugins[providerType]
		}
	}

	return nil
}

// constructedPlugin only contains EXTERNAL plugins that have been started and need to be shut down.
type constructedPlugin struct {
	Plugin inputv1.InputPluginContract
	cmd    *exec.Cmd
	// wasmPlugin is set if this is a Wasm-based plugin (cmd will be nil in this case)
	wasmPlugin *WasmInputPlugin
}

// Shutdown will loop through all _STARTED_ plugins and will send an Interrupt signal to them.
// All plugins should handle interrupt signals gracefully. For Go, this is done automatically by
// the plugin SDK.
func (r *RepositoryRegistry) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	eg, ctx := errgroup.WithContext(ctx)
	for _, p := range r.constructedPlugins {
		eg.Go(func() error {
			if p.wasmPlugin != nil {
				return p.wasmPlugin.Close()
			}

			// wasm plugins don't have commands
			if p.cmd == nil {
				return nil
			}

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

func startAndReturnPlugin(ctx context.Context, r *RepositoryRegistry, plugin *types.Plugin) (inputv1.InputPluginContract, error) {
	if isWasmPlugin(plugin.Path) {
		wasmPlugin, err := NewWasmInputPlugin(ctx, plugin.Path, plugin.ID, &plugin.Config)
		if err != nil {
			return nil, fmt.Errorf("failed to create wasm plugin %s: %w", plugin.ID, err)
		}

		r.constructedPlugins[plugin.ID] = &constructedPlugin{
			Plugin:     wasmPlugin,
			wasmPlugin: wasmPlugin,
		}

		return wasmPlugin, nil
	}

	// Handle HTTP-based plugins
	if err := plugin.Cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start plugin: %s, %w", plugin.ID, err)
	}

	client, loc, err := plugins.WaitForPlugin(ctx, plugin)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for plugin to start: %w", err)
	}

	// start log streaming once the plugin is up and running.
	// use the baseCtx here from the manager here so the streaming isn't stopped when the request is stopped.
	go plugins.StartLogStreamer(context.TODO(), plugin)

	repoPlugin := NewConstructionRepositoryPlugin(client, plugin.ID, plugin.Path, plugin.Config, loc, r.capabilities[plugin.ID])
	r.constructedPlugins[plugin.ID] = &constructedPlugin{
		Plugin: repoPlugin,
		cmd:    plugin.Cmd,
	}

	// wrap the untyped internal plugin into a typed representation.
	return repoPlugin, nil
}

// isWasmPlugin checks if the plugin path points to a Wasm file.
func isWasmPlugin(path string) bool {
	return filepath.Ext(path) == ".wasm"
}
