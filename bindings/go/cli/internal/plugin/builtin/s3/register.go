package s3

import (
	"fmt"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
	s3input "ocm.software/open-component-model/bindings/go/s3/input"
	s3repository "ocm.software/open-component-model/bindings/go/s3/repository"
	s3creds "ocm.software/open-component-model/bindings/go/s3/spec/credentials"
)

// Register wires the S3 input method, resource repository, digest processor and
// credential scheme into the CLI plugin registries.
func Register(inputRegistry *input.RepositoryRegistry,
	resourcePluginRegistry *resource.ResourceRegistry,
	digestProcessorRegistry *digestprocessor.RepositoryRegistry,
	credentialRepository *credentialrepository.RepositoryRegistry,
	httpConfig *httpv1alpha1.Config,
	filesystemConfig *filesystemv1alpha1.Config,
) error {
	var tempFolder string
	if filesystemConfig.TempFolder != nil {
		tempFolder = *filesystemConfig.TempFolder
	}

	credentialRepository.Register(s3creds.Scheme)

	method := &s3input.InputMethod{
		TempFolder: tempFolder,
		HTTPConfig: httpConfig,
	}
	if err := inputRegistry.RegisterInternalResourceInputPlugin(method); err != nil {
		return fmt.Errorf("could not register s3 resource input method: %w", err)
	}

	// WithHTTPConfig rather than a prebuilt client: the repository switches off
	// transport retry for the client it hands to the SDK, which retries on its own.
	repository := s3repository.NewResourceRepository(filesystemConfig, s3repository.WithHTTPConfig(httpConfig))
	if err := resourcePluginRegistry.RegisterInternalResourcePlugin(repository); err != nil {
		return fmt.Errorf("could not register s3 resource repository plugin: %w", err)
	}
	if err := digestProcessorRegistry.RegisterInternalDigestProcessorPlugin(repository); err != nil {
		return fmt.Errorf("could not register s3 digest processor plugin: %w", err)
	}

	return nil
}
