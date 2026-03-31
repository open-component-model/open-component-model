package clicommand

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"

	clicommandv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/clicommand/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

// commandKey is the lookup key for a registered command.
func commandKey(verb, objectType string) string {
	return verb + "/" + objectType
}

// Registry holds all plugins that provide CLI command capabilities.
type Registry struct {
	ctx context.Context
	mu  sync.Mutex

	// commands maps commandKey → plugin + spec
	commands map[string]registeredCommand
	// constructedPlugins maps plugin ID → started plugin instance
	constructedPlugins map[string]*constructedPlugin
	// capabilities maps plugin ID → its full capability spec (for multi-command plugins)
	capabilities map[string]clicommandv1.CapabilitySpec
	// internalPlugins maps commandKey → builtin implementation
	internalPlugins map[string]clicommandv1.CLICommandPluginContract
}

type registeredCommand struct {
	plugin types.Plugin
	spec   clicommandv1.CommandSpec
}

type constructedPlugin struct {
	Plugin clicommandv1.CLICommandPluginContract
	cmd    *exec.Cmd
}

// NewCLICommandRegistry creates a new Registry.
func NewCLICommandRegistry(ctx context.Context) *Registry {
	return &Registry{
		ctx:                ctx,
		commands:           make(map[string]registeredCommand),
		constructedPlugins: make(map[string]*constructedPlugin),
		capabilities:       make(map[string]clicommandv1.CapabilitySpec),
		internalPlugins:    make(map[string]clicommandv1.CLICommandPluginContract),
	}
}

// AddPlugin registers an external plugin binary for the commands it advertises.
func (r *Registry) AddPlugin(plugin types.Plugin, cap *clicommandv1.CapabilitySpec) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.capabilities[plugin.ID]; ok {
		return fmt.Errorf("plugin with ID %s already registered", plugin.ID)
	}
	r.capabilities[plugin.ID] = *cap

	for _, cmdSpec := range cap.SupportedCommands {
		key := commandKey(cmdSpec.Verb, cmdSpec.ObjectType)
		if existing, ok := r.commands[key]; ok {
			return fmt.Errorf("command %q already registered by plugin %s", key, existing.plugin.ID)
		}
		r.commands[key] = registeredCommand{plugin: plugin, spec: cmdSpec}
	}

	return nil
}

// RegisterInternalCLICommandPlugin registers a builtin (in-process) CLI command.
func (r *Registry) RegisterInternalCLICommandPlugin(spec clicommandv1.CommandSpec, contract clicommandv1.CLICommandPluginContract) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := commandKey(spec.Verb, spec.ObjectType)
	if _, ok := r.internalPlugins[key]; ok {
		return fmt.Errorf("internal command %q already registered", key)
	}
	r.internalPlugins[key] = contract
	// Also store the spec so ListCommands can return it.
	r.commands[key] = registeredCommand{spec: spec}
	return nil
}

// ListCommands returns the specs of all registered commands.
func (r *Registry) ListCommands() []clicommandv1.CommandSpec {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]clicommandv1.CommandSpec, 0, len(r.commands))
	for _, rc := range r.commands {
		out = append(out, rc.spec)
	}
	return out
}

// GetPlugin returns a ready-to-use CLICommandPluginContract for the given verb/objectType.
func (r *Registry) GetPlugin(ctx context.Context, verb, objectType string) (clicommandv1.CLICommandPluginContract, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := commandKey(verb, objectType)

	// Internal plugin wins.
	if p, ok := r.internalPlugins[key]; ok {
		return p, nil
	}

	rc, ok := r.commands[key]
	if !ok {
		return nil, fmt.Errorf("no CLI command plugin registered for %q", key)
	}

	return r.startAndGet(ctx, &rc)
}

func (r *Registry) startAndGet(ctx context.Context, rc *registeredCommand) (clicommandv1.CLICommandPluginContract, error) {
	if existing, ok := r.constructedPlugins[rc.plugin.ID]; ok {
		return existing.Plugin, nil
	}

	if err := rc.plugin.Cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start plugin %s: %w", rc.plugin.ID, err)
	}

	client, loc, err := plugins.WaitForPlugin(ctx, &rc.plugin)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for plugin %s to start: %w", rc.plugin.ID, err)
	}

	go plugins.StartLogStreamer(r.ctx, &rc.plugin)

	cap := r.capabilities[rc.plugin.ID]
	instance := newCLICommandPlugin(client, rc.plugin.ID, rc.plugin.Config, loc, cap)
	r.constructedPlugins[rc.plugin.ID] = &constructedPlugin{
		Plugin: instance,
		cmd:    rc.plugin.Cmd,
	}

	return instance, nil
}

// Shutdown sends an interrupt to all running plugin processes.
func (r *Registry) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var errs error
	for _, p := range r.constructedPlugins {
		if err := p.cmd.Process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}
