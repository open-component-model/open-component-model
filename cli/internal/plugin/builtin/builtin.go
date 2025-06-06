package builtin

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/plugin/manager"
	ocicredentialplugin "ocm.software/open-component-model/cli/internal/plugin/builtin/credentials/oci"
	ctfplugin "ocm.software/open-component-model/cli/internal/plugin/builtin/ctf"
	filev1 "ocm.software/open-component-model/cli/internal/plugin/builtin/input/file/v1"
	ociplugin "ocm.software/open-component-model/cli/internal/plugin/builtin/oci"
)

func Register(manager *manager.PluginManager) error {
	if err := ocicredentialplugin.Register(manager.CredentialRepositoryRegistry); err != nil {
		return fmt.Errorf("could not register OCI inbuilt credential plugin: %w", err)
	}

	if err := ociplugin.Register(
		manager.ComponentVersionRepositoryRegistry,
		manager.ResourceRepositoryRegistry,
	); err != nil {
		return fmt.Errorf("could not register OCI inbuilt plugin: %w", err)
	}

	if err := ctfplugin.Register(manager.ComponentVersionRepositoryRegistry); err != nil {
		return fmt.Errorf("could not register CTF inbuilt plugin: %w", err)
	}

	if err := filev1.Register(manager.InputRegistry); err != nil {
		return fmt.Errorf("could not register file input plugin: %w", err)
	}

	return nil
}
