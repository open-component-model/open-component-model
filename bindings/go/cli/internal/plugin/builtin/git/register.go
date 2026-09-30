package git

import (
	"fmt"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	gitinput "ocm.software/open-component-model/bindings/go/git/input"
	gitrepository "ocm.software/open-component-model/bindings/go/git/repository"
	httpclient "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialtyperepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
)

// Register wires the Git input method, resource repository, digest processor and
// credential scheme into the CLI plugin registries.
func Register(inputRegistry *input.RepositoryRegistry,
	resourcePluginRegistry *resource.ResourceRegistry,
	digestProcessorRegistry *digestprocessor.RepositoryRegistry,
	credentialTypeRegistry *credentialtyperepository.CredentialTypeRegistry,
	filesystemConfig *filesystemv1alpha1.Config,
	httpConfig *httpv1alpha1.Config,
) error {
	var tempFolder string
	if filesystemConfig.TempFolder != nil {
		tempFolder = *filesystemConfig.TempFolder
	}

	method := &gitinput.InputMethod{
		TempFolder: tempFolder,
		HTTPConfig: httpConfig,
	}
	if err := inputRegistry.RegisterInternalResourceInputPlugin(method); err != nil {
		return fmt.Errorf("could not register git resource input method: %w", err)
	}
	if err := credentialTypeRegistry.RegisterInternalCredentialTypeSchemeProvider(method); err != nil {
		return fmt.Errorf("could not register git credential types: %w", err)
	}

	repository := gitrepository.NewResourceRepository(filesystemConfig, gitrepository.WithHTTPClient(httpclient.New(httpclient.WithConfig(httpConfig))))
	if err := resourcePluginRegistry.RegisterInternalResourcePlugin(repository); err != nil {
		return fmt.Errorf("could not register git resource repository plugin: %w", err)
	}
	if err := digestProcessorRegistry.RegisterInternalDigestProcessorPlugin(repository); err != nil {
		return fmt.Errorf("could not register git digest processor plugin: %w", err)
	}

	if err := credentialTypeRegistry.RegisterInternalCredentialTypeSchemeProvider(repository); err != nil {
		return fmt.Errorf("could not register git credential types: %w", err)
	}

	return nil
}
