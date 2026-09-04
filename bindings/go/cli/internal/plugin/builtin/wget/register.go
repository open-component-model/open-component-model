package wget

import (
	"fmt"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	httpclient "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialtyperepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
	wgetinput "ocm.software/open-component-model/bindings/go/wget/input"
	wgetrepository "ocm.software/open-component-model/bindings/go/wget/repository"
)

// Register wires the wget input method and its credential scheme into the CLI plugin registries.
func Register(inputRegistry *input.RepositoryRegistry,
	resourcePluginRegistry *resource.ResourceRegistry,
	digestProcessorRegistry *digestprocessor.RepositoryRegistry,
	credentialTypeRegistry *credentialtyperepository.CredentialTypeRegistry,
	httpConfig *httpv1alpha1.Config,
	filesystemConfig *filesystemv1alpha1.Config,
) error {
	var tempFolder string
	if filesystemConfig.TempFolder != nil {
		tempFolder = *filesystemConfig.TempFolder
	}
	method := &wgetinput.InputMethod{
		TempFolder: tempFolder,
		HTTPConfig: httpConfig,
	}

	if err := credentialTypeRegistry.RegisterInternalCredentialTypeSchemeProvider(method); err != nil {
		return fmt.Errorf("could not register wget credential types: %w", err)
	}

	if err := inputRegistry.RegisterInternalResourceInputPlugin(method); err != nil {
		return fmt.Errorf("could not register wget resource input method: %w", err)
	}

	wgetResourceRepository := wgetrepository.NewResourceRepository(
		filesystemConfig,
		wgetrepository.WithHTTPClient(httpclient.New(httpclient.WithConfig(httpConfig))),
	)
	if err := resourcePluginRegistry.RegisterInternalResourcePlugin(wgetResourceRepository); err != nil {
		return fmt.Errorf("could not register wget resource repository plugin: %w", err)
	}
	if err := digestProcessorRegistry.RegisterInternalDigestProcessorPlugin(wgetResourceRepository); err != nil {
		return fmt.Errorf("could not register wget digest processor plugin: %w", err)
	}

	return nil
}
