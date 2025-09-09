package builtin

import (
	"fmt"
	"log/slog"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	ocicredentialplugin "ocm.software/open-component-model/cli/internal/plugin/builtin/credentials/oci"
	ctfplugin "ocm.software/open-component-model/cli/internal/plugin/builtin/ctf"
	"ocm.software/open-component-model/cli/internal/plugin/builtin/input/dir"
	"ocm.software/open-component-model/cli/internal/plugin/builtin/input/file"
	"ocm.software/open-component-model/cli/internal/plugin/builtin/input/utf8"
	ociplugin "ocm.software/open-component-model/cli/internal/plugin/builtin/oci"
	"ocm.software/open-component-model/cli/internal/plugin/builtin/rsa"
)

func Register(manager *manager.PluginManager, filesystemConfig *filesystemv1alpha1.Config, logger *slog.Logger) error {
	if err := ocicredentialplugin.Register(manager.CredentialRepositoryRegistry); err != nil {
		return fmt.Errorf("could not register OCI inbuilt credential plugin: %w", err)
	}

	if err := ociplugin.Register(
		manager.ComponentVersionRepositoryRegistry,
		manager.ResourcePluginRegistry,
		manager.DigestProcessorRegistry,
		manager.BlobTransformerRegistry,
		filesystemConfig,
		logger,
	); err != nil {
		return fmt.Errorf("could not register OCI inbuilt plugin: %w", err)
	}

	if err := ctfplugin.Register(manager.ComponentVersionRepositoryRegistry); err != nil {
		return fmt.Errorf("could not register CTF inbuilt plugin: %w", err)
	}

	if err := file.Register(manager.InputRegistry, filesystemConfig); err != nil {
		return fmt.Errorf("could not register file input plugin: %w", err)
	}
	if err := utf8.Register(manager.InputRegistry); err != nil {
		return fmt.Errorf("could not register utf8 input plugin: %w", err)
	}
	if err := dir.Register(manager.InputRegistry, filesystemConfig); err != nil {
		return fmt.Errorf("could not register dir input plugin: %w", err)
	}
	if err := rsa.Register(manager.SigningRegistry, filesystemConfig); err != nil {
		return fmt.Errorf("could not register RSA signing plugin: %w", err)
	}

	return nil
}
