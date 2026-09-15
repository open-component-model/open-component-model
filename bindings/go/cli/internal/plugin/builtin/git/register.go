package git

import (
	"fmt"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	gitrepository "ocm.software/open-component-model/bindings/go/git/repository"
	gitcreds "ocm.software/open-component-model/bindings/go/git/spec/credentials"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
)

// Register wires the Git resource repository, digest processor and credential
// scheme into the CLI plugin registries.
func Register(resourcePluginRegistry *resource.ResourceRegistry,
	digestProcessorRegistry *digestprocessor.RepositoryRegistry,
	credentialRepository *credentialrepository.RepositoryRegistry,
	filesystemConfig *filesystemv1alpha1.Config,
) error {
	var opts []gitrepository.Option
	if filesystemConfig.TempFolder != nil {
		opts = append(opts, gitrepository.WithTempDir(*filesystemConfig.TempFolder))
	}

	credentialRepository.Register(gitcreds.Scheme)

	repository := gitrepository.NewResourceRepository(opts...)
	if err := resourcePluginRegistry.RegisterInternalResourcePlugin(repository); err != nil {
		return fmt.Errorf("could not register git resource repository plugin: %w", err)
	}
	if err := digestProcessorRegistry.RegisterInternalDigestProcessorPlugin(repository); err != nil {
		return fmt.Errorf("could not register git digest processor plugin: %w", err)
	}

	return nil
}
