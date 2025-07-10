package builtin

import (
	"fmt"
	"log/slog"

	"ocm.software/open-component-model/bindings/go/plugin/manager"
	builtinv1 "ocm.software/open-component-model/cli/internal/plugin/builtin/config/v1"
	ocicredentialplugin "ocm.software/open-component-model/cli/internal/plugin/builtin/credentials/oci"
	ctfplugin "ocm.software/open-component-model/cli/internal/plugin/builtin/ctf"
	"ocm.software/open-component-model/cli/internal/plugin/builtin/input/file"
	"ocm.software/open-component-model/cli/internal/plugin/builtin/input/utf8"
	ociplugin "ocm.software/open-component-model/cli/internal/plugin/builtin/oci"
)

// Register registers built-in plugins with the plugin manager using the provided configuration.
func Register(manager *manager.PluginManager, pluginConfig *builtinv1.BuiltinPluginConfig, logger *slog.Logger) error {
	// Register OCI credential plugin
	if err := ocicredentialplugin.Register(manager.CredentialRepositoryRegistry); err != nil {
		return fmt.Errorf("could not register OCI inbuilt credential plugin: %w", err)
	}

	// Register OCI plugin with configuration
	if err := ociplugin.Register(
		manager.ComponentVersionRepositoryRegistry,
		manager.ResourcePluginRegistry,
		manager.DigestProcessorRegistry,
		pluginConfig,
		logger,
	); err != nil {
		return fmt.Errorf("could not register OCI inbuilt plugin: %w", err)
	}

	// Register CTF plugin with configuration
	if err := ctfplugin.Register(manager.ComponentVersionRepositoryRegistry, pluginConfig, logger); err != nil {
		return fmt.Errorf("could not register CTF inbuilt plugin: %w", err)
	}

	// Register input plugins
	if err := file.Register(manager.InputRegistry); err != nil {
		return fmt.Errorf("could not register file input plugin: %w", err)
	}
	if err := utf8.Register(manager.InputRegistry); err != nil {
		return fmt.Errorf("could not register utf8 input plugin: %w", err)
	}

	return nil
}
