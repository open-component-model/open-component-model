package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const socketPathFormat = "/tmp/ocm_plugin_%s.sock"

// ImplementedPlugin contains information about a plugin that has been included via direct implementation.
type ImplementedPlugin struct {
	Base         PluginBase
	Capabilities []Capability
	Type         string
	ID           string
}

// implementedRegisteredPlugins will contain plugins that have been included via direct implementation for a
// specific type and capability.
var implementedRegisteredPlugins = map[string]map[string][]*ImplementedPlugin{}

// RegisterPluginImplementationForTypeAndCapabilities can be called by actual implementations in the source.
// It will register any implementations directly for a given type and capability.
func RegisterPluginImplementationForTypeAndCapabilities(p *ImplementedPlugin) {
	for _, capability := range p.Capabilities {
		if _, ok := implementedRegisteredPlugins[p.Type]; !ok {
			implementedRegisteredPlugins[p.Type] = map[string][]*ImplementedPlugin{}
		}

		implementedRegisteredPlugins[p.Type][capability.Capability] = append(implementedRegisteredPlugins[p.Type][capability.Capability], p)
	}
}

// Plugin represents a connected plugin
type Plugin struct {
	ID     string
	path   string
	config Config

	cmd *exec.Cmd
}

// constructedPlugin is a plugin that has been created and stored before actually starting it.
type constructedPlugin struct {
	Plugin any

	cmd *exec.Cmd
}

// PluginManager manages all connected plugins.
type PluginManager struct {
	// Stores plugins for each capability. Capabilities are determined
	// through the plugins.
	// A plugin contains their capability. When looking for a plugin
	// we loop through all types and see if a plugin supports the
	// needed capability or all defined capabilities.
	plugins map[string]map[string][]*Plugin
	mu      sync.Mutex

	// This tracks plugins that are not _started_ and have been requested.
	// The number of used plugins can differ considerably compared to
	// the actual registered plugins.
	// This is separate from the plugins being registered because we don't want
	// to always loop through all the registered plugins and check their state.
	// For example, during shutdown or during checking if we already have a started
	// plugin or not.
	constructedPlugins map[string]*constructedPlugin
	logger             *slog.Logger

	// baseCtx is the context that is used for all plugins.
	// This is a different context than the one used for fetching plugins because
	// that context is done once fetching is done. The plugin context, however, must not
	// be cancelled.
	baseCtx context.Context
}

// NewPluginManager initializes the PluginManager
// the passed ctx is used for all plugins.
func NewPluginManager(ctx context.Context, logger *slog.Logger) *PluginManager {
	return &PluginManager{
		baseCtx:            ctx,
		logger:             logger,
		plugins:            make(map[string]map[string][]*Plugin),
		constructedPlugins: make(map[string]*constructedPlugin),
	}
}

// fetchPlugin has so many parameters because generics isn't allowed on receiver types
// therefore we pass in everything from the plugin manager.
func fetchPlugin[T PluginBase](
	ctx context.Context,
	typ runtime.Typed,
	requiredCapability string,
	pm *PluginManager,
) ([]T, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pluginsMap := make(map[string]T)

	// Look for source implemented plugins as well.
	if implementedCaps, ok := implementedRegisteredPlugins[typ.GetType().String()]; ok {
		// if we found registered plugins, add them to the map
		plugins, ok := implementedCaps[requiredCapability]
		if ok {
			for _, plugin := range plugins {
				t, ok := plugin.Base.(T)
				if !ok {
					return nil, fmt.Errorf("expected type %T but got %T", t, plugin.Base)
				}

				pluginsMap[plugin.ID] = t
			}

			// Return any implementations that are registered and look no further.
			var result []T
			for plugin := range maps.Values(pluginsMap) {
				result = append(result, plugin)
			}

			return result, nil
		}
	}

	// anything after implemented plugins using the same ID may overwrite existing registrations.
	caps, ok := pm.plugins[typ.GetType().String()]
	if !ok {
		return []T{}, fmt.Errorf("unknown plugin type: %s, known are %v", typ.GetType().String(), slices.Collect(maps.Keys(pm.plugins)))
	}

	foundPlugins, ok := caps[requiredCapability]
	if !ok {
		return []T{}, fmt.Errorf("required capability not found in capabilities: %s", requiredCapability)
	}

	for _, p := range foundPlugins {
		// Call the right schema and call validate on it, and change the api on the type not being just a String.
		if validate, err := validatePlugin(p, typ, requiredCapability); validate && err != nil {
			return nil, err
		}

		// Check if we already constructed this plugin and return it.
		if existingPlugin, ok := pm.constructedPlugins[p.ID]; ok {
			t, ok := existingPlugin.Plugin.(T)
			if !ok {
				return nil, fmt.Errorf("expected type %T but got %T", t, p)
			}

			pluginsMap[p.ID] = t
		} else {
			if err := p.cmd.Start(); err != nil {
				return nil, fmt.Errorf("failed to start plugin: %s, %w", p.ID, err)
			}

			client, err := waitForPlugin(ctx, p.ID, p.config.Location, p.config.Type)
			if err != nil {
				return nil, fmt.Errorf("failed to wait for plugin to start: %w", err)
			}

			// create the base plugin backed by a concrete implementation of plugin interfaces.
			var plugin PluginBase = NewRepositoryPlugin(pm.baseCtx, pm.logger, client, p.ID, p.path, p.config)
			t, ok := plugin.(T)
			if !ok {
				return nil, fmt.Errorf("expected type %T but got %T", t, p)
			}

			pluginsMap[p.ID] = t

			pm.constructedPlugins[p.ID] = &constructedPlugin{
				Plugin: t,
				cmd:    p.cmd,
			}
		}
	}

	if len(pluginsMap) == 0 {
		return nil, fmt.Errorf("no plugin(s) available for type %s with capability: %s", typ, requiredCapability)
	}

	var plugins []T
	for plugin := range maps.Values(pluginsMap) {
		plugins = append(plugins, plugin)
	}

	return plugins, nil
}

func getDirectlyRegisteredPlugins() {

}

// only run validation if a schema exists.
func validatePlugin(p *Plugin, typ runtime.Typed, requiredCapability string) (bool, error) {
	c := jsonschema.NewCompiler()
	accessSpec, ok := p.config.AccessSpec[typ.GetType().String()]
	if !ok {
		// skip if no access spec is defined
		return false, nil
	}

	schema, ok := accessSpec[requiredCapability]
	if !ok {
		// skip if there is no access spec for this capability
		return false, nil
	}

	unmarshaler, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema))
	var v any
	if err := json.Unmarshal(schema, &v); err != nil {
		return true, err
	}

	if err := c.AddResource("schema.json", unmarshaler); err != nil {
		return true, fmt.Errorf("failed to add schema.json: %w", err)
	}
	sch, err := c.Compile("schema.json")
	if err != nil {
		return true, fmt.Errorf("failed to compile schema.json: %w", err)
	}

	// need to marshal the interface into a JSON format.
	content, err := json.Marshal(typ)
	if err != nil {
		return true, fmt.Errorf("failed to marshal type: %w", err)
	}
	// once marshalled, we create a map[string]any representation of the marshaled content.
	unmarshalledType, err := jsonschema.UnmarshalJSON(bytes.NewReader(content))
	if err != nil {
		return true, fmt.Errorf("failed to unmarshal : %w", err)
	}

	if _, ok := unmarshalledType.(string); ok {
		// TODO: In _not_ POC this should be either a type switch, or some kind of exclusion or we should change how
		// we register and look up plugins to avoid validating when listing or for certain plugins.
		// skip validation if the passed in type is of type string.
		return true, nil
	}

	// finally, validate map[string]any against the loaded schema
	if err := sch.Validate(unmarshalledType); err != nil {
		var typRaw bytes.Buffer
		err = errors.Join(err, json.Indent(&typRaw, content, "", "  "))
		var schemaRaw bytes.Buffer
		err = errors.Join(err, json.Indent(&schemaRaw, schema, "", "  "))
		return true, fmt.Errorf("failed to validate schema for\n%s\n---SCHEMA---\n%s\n: %w", typRaw.String(), schemaRaw.String(), err)
	}

	return true, nil
}

type RegistrationOptions struct {
	IdleTimeout time.Duration
}

type RegistrationOptionFn func(*RegistrationOptions)

func WithIdleTimeout(d time.Duration) RegistrationOptionFn {
	return func(o *RegistrationOptions) {
		o.IdleTimeout = d
	}
}

// RegisterPluginsAtLocation walks through files in a folder and registers them
// as plugins if connection points can be established. This function doesn't support
// concurrent access.
func (pm *PluginManager) RegisterPluginsAtLocation(ctx context.Context, dir string, opts ...RegistrationOptionFn) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	defaultOpts := &RegistrationOptions{
		IdleTimeout: time.Hour,
	}

	for _, opt := range opts {
		opt(defaultOpts)
	}

	conf := &Config{
		IdleTimeout: &defaultOpts.IdleTimeout,
	}

	t, err := determineConnectionType()
	if err != nil {
		return fmt.Errorf("could not determine connection type: %w", err)
	}
	conf.Type = t
	conf.AccessSpec = make(map[string]map[string][]byte)

	var plugins []*Plugin
	if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// TODO: Determine plugin extension.
		ext := filepath.Ext(info.Name())
		if ext != "" {
			return nil
		}

		id := filepath.Base(path)

		p := &Plugin{
			ID:     id,
			path:   path,
			config: *conf,
		}

		pm.logger.DebugContext(ctx, "discovered plugin", "id", id, "path", path)

		plugins = append(plugins, p)

		return nil
	}); err != nil {
		return fmt.Errorf("failed to discover plugins: %w", err)
	}

	for _, plugin := range plugins {
		location, err := determineConnectionLocation(plugin)
		if err != nil {
			return fmt.Errorf("failed to determine connection location: %w", err)
		}
		conf.Location = location
		conf.ID = plugin.ID
		plugin.config = *conf

		output := bytes.NewBuffer(nil)
		cmd := exec.CommandContext(ctx, plugin.path, "capabilities")
		cmd.Stdout = output
		cmd.Stderr = os.Stderr

		// Use Wait so we get the capabilities and make sure that the command exists and returns the values we need.
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to start plugin %s: %w", plugin.ID, err)
		}

		caps := &Capabilities{}
		if err := json.Unmarshal(output.Bytes(), caps); err != nil {
			return fmt.Errorf("failed to unmarshal capabilities: %w", err)
		}

		// set up schemas for the plugin config
		for typ := range caps.Type {
			if plugin.config.AccessSpec[typ.String()] == nil {
				plugin.config.AccessSpec[typ.String()] = make(map[string][]byte)
			}

			for _, c := range caps.Type[typ] {
				plugin.config.AccessSpec[typ.String()][c.Capability] = c.Schema
			}
		}

		serialized, err := json.Marshal(plugin.config)
		if err != nil {
			return err
		}

		// Create a command that can then be managed.
		pluginCmd := exec.CommandContext(ctx, plugin.path, "--config", string(serialized))
		pluginCmd.Stdout = os.Stdout
		pluginCmd.Stderr = os.Stdout
		pluginCmd.Cancel = func() error {
			slog.Info("killing plugin process because the parent context is cancelled", "id", plugin.ID)
			return cmd.Process.Kill()
		}
		plugin.cmd = pluginCmd

		for typ, caps := range caps.Type {
			for _, c := range caps {
				if pm.plugins[typ.String()] == nil {
					pm.plugins[typ.String()] = make(map[string][]*Plugin)
				}

				pm.plugins[typ.String()][c.Capability] = append(pm.plugins[typ.String()][c.Capability], plugin)
			}
		}
	}

	return nil
}

// Shutdown is called to terminate all plugins.
func (pm *PluginManager) Shutdown(ctx context.Context) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	var errs error
	for _, p := range pm.constructedPlugins {
		// The plugins should handle the Interrupt signal for shutdowns.
		// TODO: Maybe wait for the plugin to actually shutdown?
		if perr := p.cmd.Process.Signal(os.Interrupt); perr != nil {
			errs = errors.Join(errs, perr)
		}
	}

	return errs
}

func determineConnectionLocation(plugin *Plugin) (_ string, err error) {
	switch plugin.config.Type {
	case TCP:
		listener, err := net.Listen("tcp", ":0")
		if err != nil {
			return "", err
		}

		defer func() {
			if lerr := listener.Close(); lerr != nil {
				err = errors.Join(err, lerr)
			}
		}()

		return listener.Addr().String(), nil
	case Socket:
		return fmt.Sprintf(socketPathFormat, plugin.ID), nil
	}

	return "", fmt.Errorf("unknown plugin connection type: %s", plugin.config.Type)
}

func determineConnectionType() (ConnectionType, error) {
	tmp, err := os.MkdirTemp("", "")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary directory: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tmp)
	}()

	socketPath := filepath.Join(tmp, "plugin.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return TCP, nil
	}

	if err := listener.Close(); err != nil {
		return "", fmt.Errorf("failed to close socket: %w", err)
	}

	return Socket, nil
}
