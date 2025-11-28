package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	blobtransformerv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/blobtransformer/v1"
	componentlisterv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/componentlister/v1"
	credentialrepositoryv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentials/v1"
	digestprocessorv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/digestprocessor/v1"
	inputv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/input/v1"
	ocmrepositoryv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	resourcev1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/resource/v1"
	signinghandlerv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/signing/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/blobtransformer"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentlister"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	pluginruntime "ocm.software/open-component-model/bindings/go/plugin/manager/types/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ErrNoPluginsFound is returned when a register plugin call finds no plugins.
var ErrNoPluginsFound = errors.New("no plugins found")

// PluginManager manages all connected plugins.
type PluginManager struct {
	// Registries containing various typed plugins. These should be called directly using the
	// plugin manager to locate a required plugin.
	ComponentVersionRepositoryRegistry *componentversionrepository.RepositoryRegistry
	ComponentListerRegistry            *componentlister.ComponentListerRegistry
	CredentialRepositoryRegistry       *credentialrepository.RepositoryRegistry
	InputRegistry                      *input.RepositoryRegistry
	DigestProcessorRegistry            *digestprocessor.RepositoryRegistry
	ResourcePluginRegistry             *resource.ResourceRegistry
	BlobTransformerRegistry            *blobtransformer.Registry
	SigningRegistry                    *signinghandler.SigningRegistry

	mu sync.Mutex

	// baseCtx is the context that is used for all plugins.
	// This is a different context than the one used for fetching plugins because
	// that context is done once fetching is done. The plugin context, however, must not
	// be cancelled.
	baseCtx context.Context
}

// NewPluginManager initializes the PluginManager
// the passed ctx is used for all plugins.
func NewPluginManager(ctx context.Context) *PluginManager {
	return &PluginManager{
		ComponentVersionRepositoryRegistry: componentversionrepository.NewComponentVersionRepositoryRegistry(ctx),
		ComponentListerRegistry:            componentlister.NewComponentListerRegistry(ctx),
		CredentialRepositoryRegistry:       credentialrepository.NewCredentialRepositoryRegistry(ctx),
		InputRegistry:                      input.NewInputRepositoryRegistry(ctx),
		DigestProcessorRegistry:            digestprocessor.NewDigestProcessorRegistry(ctx),
		ResourcePluginRegistry:             resource.NewResourceRegistry(ctx),
		BlobTransformerRegistry:            blobtransformer.NewBlobTransformerRegistry(ctx),
		SigningRegistry:                    signinghandler.NewSigningRegistry(ctx),
		baseCtx:                            ctx,
	}
}

type RegistrationOptions struct {
	IdleTimeout time.Duration
	Config      *genericv1.Config
}

type RegistrationOptionFn func(*RegistrationOptions)

// WithIdleTimeout configures the maximum amount of time for a plugin to quit if it's idle.
func WithIdleTimeout(d time.Duration) RegistrationOptionFn {
	return func(o *RegistrationOptions) {
		o.IdleTimeout = d
	}
}

// WithConfiguration adds a configuration to the plugin.
func WithConfiguration(c *genericv1.Config) RegistrationOptionFn {
	return func(o *RegistrationOptions) {
		o.Config = c
	}
}

// RegisterPlugins walks through files in a folder and registers them
// as plugins if connection points can be established. This function doesn't support
// concurrent access.
func (pm *PluginManager) RegisterPlugins(ctx context.Context, dir string, opts ...RegistrationOptionFn) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	defaultOpts := &RegistrationOptions{
		IdleTimeout: time.Hour,
	}

	for _, opt := range opts {
		opt(defaultOpts)
	}

	conf := &mtypes.Config{
		IdleTimeout: &defaultOpts.IdleTimeout,
	}

	t, err := determineConnectionType(ctx)
	if err != nil {
		return fmt.Errorf("could not determine connection type: %w", err)
	}
	conf.Type = t

	plugins, err := pm.fetchPlugins(ctx, conf, dir)
	if err != nil {
		return fmt.Errorf("could not fetch plugins: %w", err)
	}

	if len(plugins) == 0 {
		return ErrNoPluginsFound
	}

	for _, plugin := range plugins {
		conf.ID = plugin.ID
		plugin.Config = *conf

		output := bytes.NewBuffer(nil)
		// TODO(fabianburth): provide developer documentation on how to debug
		//   plugins.
		// The commented command below can be used to start the plugin in headless mode with Delve for debugging purposes.
		// Depending on your IDE, you can create a remote debugging configuration that connects to the specified port.
		// The execution will wait for a debugger to attach before proceeding.
		// cmd := exec.CommandContext(ctx, "dlv", "exec", cleanPath(plugin.Path), "--headless=true", "--listen=:40000", "--api-version=2", "--accept-multiclient", "--log", "--log-dest=2", "--", "capabilities")
		cmd := exec.CommandContext(ctx, cleanPath(plugin.Path), "capabilities") //nolint:gosec // G204 does not apply
		cmd.Stdout = output
		cmd.Stderr = os.Stderr

		// Use Wait so we get the capabilities and make sure that the command exists and returns the values we need.
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to start plugin %s: %w", plugin.ID, err)
		}

		if err := pm.addPlugin(pm.baseCtx, defaultOpts.Config, *plugin, output); err != nil {
			return fmt.Errorf("failed to add plugin %s: %w", plugin.ID, err)
		}
	}

	return nil
}

func cleanPath(path string) string {
	return strings.Trim(path, `,;:'"|&*!@#$`)
}

// Shutdown is called to terminate all plugins.
func (pm *PluginManager) Shutdown(ctx context.Context) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	var errs error

	errs = errors.Join(errs,
		pm.ComponentVersionRepositoryRegistry.Shutdown(ctx),
		pm.ComponentListerRegistry.Shutdown(ctx),
		pm.CredentialRepositoryRegistry.Shutdown(ctx),
		pm.InputRegistry.Shutdown(ctx),
		pm.DigestProcessorRegistry.Shutdown(ctx),
		pm.ResourcePluginRegistry.Shutdown(ctx),
		pm.BlobTransformerRegistry.Shutdown(ctx),
		pm.SigningRegistry.Shutdown(ctx),
	)

	return errs
}

func (pm *PluginManager) fetchPlugins(ctx context.Context, conf *mtypes.Config, dir string) ([]*mtypes.Plugin, error) {
	var plugins []*mtypes.Plugin
	if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if os.IsNotExist(err) {
			return ErrNoPluginsFound
		}

		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// TODO(Skarlso): Determine plugin extension.
		ext := filepath.Ext(info.Name())
		if ext != "" {
			return nil
		}

		id := filepath.Base(path)

		p := &mtypes.Plugin{
			ID:     id,
			Path:   path,
			Config: *conf,
		}

		slog.DebugContext(ctx, "discovered plugin", "id", id, "path", path)

		plugins = append(plugins, p)

		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to discover plugins: %w", err)
	}

	return plugins, nil
}

var scheme *runtime.Scheme

// if we add another capability type, we need to register it here.
// ATTENTION: keep in sync with switch case statement below
// if you add a new capability type, make sure to register its scheme here.
func init() {
	scheme = runtime.NewScheme()
	scheme.MustRegisterScheme(ocmrepositoryv1.Scheme)
	scheme.MustRegisterScheme(blobtransformerv1.Scheme)
	scheme.MustRegisterScheme(credentialrepositoryv1.Scheme)
	scheme.MustRegisterScheme(componentlisterv1.Scheme)
	scheme.MustRegisterScheme(digestprocessorv1.Scheme)
	scheme.MustRegisterScheme(inputv1.Scheme)
	scheme.MustRegisterScheme(resourcev1.Scheme)
	scheme.MustRegisterScheme(signinghandlerv1.Scheme)
}

func (pm *PluginManager) addPlugin(ctx context.Context, ocmConfig *genericv1.Config, plugin mtypes.Plugin, capabilitiesCommandOutput *bytes.Buffer) error {
	// Determine Configuration requirements.
	rawPluginSpec := spec.PluginSpec{}
	if err := json.Unmarshal(capabilitiesCommandOutput.Bytes(), &rawPluginSpec); err != nil {
		return fmt.Errorf("failed to unmarshal capabilities: %w", err)
	}
	pluginSpec, err := pluginruntime.ConvertFromSpec(scheme, &rawPluginSpec)
	if err != nil {
		return fmt.Errorf("failed to convert plugin spec: %w", err)
	}

	if ocmConfig != nil {
		filtered, _ := genericv1.Filter(ocmConfig, &genericv1.FilterOptions{ConfigTypes: pluginSpec.SupportedConfigTypes})
		if len(pluginSpec.SupportedConfigTypes) > 0 && len(filtered.Configurations) == 0 {
			return fmt.Errorf("no configuration found for plugin %s; requested configuration types: %s", plugin.ID, pluginSpec.SupportedConfigTypes)
		}

		plugin.Config.ConfigTypes = append(plugin.Config.ConfigTypes, filtered.Configurations...)
	}

	serialized, err := json.Marshal(plugin.Config)
	if err != nil {
		return err
	}

	// Create a command that can then be managed.
	pluginCmd := exec.CommandContext(ctx, cleanPath(plugin.Path), "--config", string(serialized)) //nolint:gosec // G204 does not apply
	pluginCmd.Cancel = func() error {
		slog.InfoContext(ctx, "killing plugin process because the parent context is cancelled", "id", plugin.ID)
		return pluginCmd.Process.Kill()
	}

	// Set up communication pipes.
	plugin.Cmd = pluginCmd
	sdtErr, err := pluginCmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}
	plugin.Stderr = sdtErr
	sdtOut, err := pluginCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	plugin.Stdout = sdtOut

	// TODO(fabianburth): all registries have a common interface now
	//  we could refactor this to get rid of the switch case statement.
	for _, capability := range pluginSpec.CapabilitySpecs {
		switch capability := capability.(type) {
		case *ocmrepositoryv1.CapabilitySpec:
			slog.DebugContext(ctx, "adding component version repository plugin", "id", plugin.ID)
			if err := pm.ComponentVersionRepositoryRegistry.AddPlugin(plugin, capability); err != nil {
				return fmt.Errorf("failed to register plugin %s: %w", plugin.ID, err)
			}
		case *blobtransformerv1.CapabilitySpec:
			slog.DebugContext(ctx, "adding blob transformer plugin", "id", plugin.ID)
			if err := pm.BlobTransformerRegistry.AddPlugin(plugin, capability); err != nil {
				return fmt.Errorf("failed to register plugin %s: %w", plugin.ID, err)
			}
		case *credentialrepositoryv1.CapabilitySpec:
			slog.DebugContext(ctx, "adding credential repository plugin", "id", plugin.ID)
			if err := pm.CredentialRepositoryRegistry.AddPlugin(plugin, capability); err != nil {
				return fmt.Errorf("failed to register plugin %s: %w", plugin.ID, err)
			}
		case *componentlisterv1.CapabilitySpec:
			slog.DebugContext(ctx, "adding component lister plugin", "id", plugin.ID)
			if err := pm.ComponentListerRegistry.AddPlugin(plugin, capability); err != nil {
				return fmt.Errorf("failed to register plugin %s: %w", plugin.ID, err)
			}
		case *digestprocessorv1.CapabilitySpec:
			slog.DebugContext(ctx, "adding digest processor plugin", "id", plugin.ID)
			if err := pm.DigestProcessorRegistry.AddPlugin(plugin, capability); err != nil {
				return fmt.Errorf("failed to register plugin %s: %w", plugin.ID, err)
			}
		case *inputv1.CapabilitySpec:
			slog.DebugContext(ctx, "adding construction resource input plugin", "id", plugin.ID)
			if err := pm.InputRegistry.AddPlugin(plugin, capability); err != nil {
				return fmt.Errorf("failed to register plugin %s: %w", plugin.ID, err)
			}
		case *resourcev1.CapabilitySpec:
			slog.DebugContext(ctx, "adding resource repository plugin", "id", plugin.ID)
			if err := pm.ResourcePluginRegistry.AddPlugin(plugin, capability); err != nil {
				return fmt.Errorf("failed to register plugin %s: %w", plugin.ID, err)
			}
		case *signinghandlerv1.CapabilitySpec:
			slog.DebugContext(ctx, "adding signing handler plugin", "id", plugin.ID)
			if err := pm.SigningRegistry.AddPlugin(plugin, capability); err != nil {
				return fmt.Errorf("failed to register plugin %s: %w", plugin.ID, err)
			}
		default:
			return fmt.Errorf("unknown capability type %T for plugin %s", capability, plugin.ID)
		}
	}

	return nil
}

func determineConnectionType(ctx context.Context) (mtypes.ConnectionType, error) {
	// if we can't create a temp folder ( for example we are in a scratch container ) we default to TCP
	tmp, err := os.MkdirTemp("", "")
	if err != nil {
		slog.DebugContext(ctx, "failed to create temporary folder, falling back to TCP connection", "err", err.Error())
		return mtypes.TCP, nil
	}

	defer func() {
		_ = os.RemoveAll(tmp)
	}()

	socketPath := filepath.Join(tmp, "plugin.sock")
	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "unix", socketPath)
	if err != nil {
		return mtypes.TCP, nil
	}

	if err := listener.Close(); err != nil {
		return "", fmt.Errorf("failed to close socket: %w", err)
	}

	return mtypes.Socket, nil
}
