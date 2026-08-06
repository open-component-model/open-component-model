package github

import (
	"fmt"

	githubdigest "ocm.software/open-component-model/bindings/go/github/digest"
	githubrepository "ocm.software/open-component-model/bindings/go/github/repository/resource"
	githubcreds "ocm.software/open-component-model/bindings/go/github/spec/credentials"
	httpclient "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
)

// Register wires the GitHub resource repository, its digest processor and its
// credential scheme into the CLI plugin registries.
func Register(resourcePluginRegistry *resource.ResourceRegistry,
	digestProcessorRegistry *digestprocessor.RepositoryRegistry,
	credentialRepository *credentialrepository.RepositoryRegistry,
	httpConfig *httpv1alpha1.Config,
) error {
	httpClient := httpclient.New(httpclient.WithConfig(httpConfig))

	credentialRepository.Register(githubcreds.Scheme)

	repository := githubrepository.NewResourceRepository(githubrepository.WithHTTPClient(httpClient))
	if err := resourcePluginRegistry.RegisterInternalResourcePlugin(repository); err != nil {
		return fmt.Errorf("could not register github resource repository plugin: %w", err)
	}

	digestProcessor := githubdigest.NewDigestProcessor(githubrepository.WithHTTPClient(httpClient))
	if err := digestProcessorRegistry.RegisterInternalDigestProcessorPlugin(digestProcessor); err != nil {
		return fmt.Errorf("could not register github digest processor plugin: %w", err)
	}

	return nil
}
