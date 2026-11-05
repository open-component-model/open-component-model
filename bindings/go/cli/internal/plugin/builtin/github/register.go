package github

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/credentials"
	githubdigest "ocm.software/open-component-model/bindings/go/github/digest"
	githubrepository "ocm.software/open-component-model/bindings/go/github/repository/resource"
	httpclient "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialtyperepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
)

// RegisterCredentialTypes declares the credential types the GitHub binding consumes. The
// credential graph ingests a configuration against them, so they are registered before it is
// built, while Register needs the finished graph. The declared scheme is the binding's own and
// does not depend on how a repository is configured, so a bare one serves to declare it.
func RegisterCredentialTypes(credentialTypeRegistry *credentialtyperepository.CredentialTypeRegistry) error {
	if err := credentialTypeRegistry.RegisterInternalCredentialTypeSchemeProvider(githubrepository.NewResourceRepository()); err != nil {
		return fmt.Errorf("could not register github credential types: %w", err)
	}
	if err := credentialTypeRegistry.RegisterInternalCredentialTypeSchemeProvider(githubdigest.NewDigestProcessor()); err != nil {
		return fmt.Errorf("could not register github credential types: %w", err)
	}
	return nil
}

// Register wires the GitHub resource repository and its digest processor into the CLI plugin
// registries. credentialResolver is the finished credential graph, which they consult for a
// host the access addresses beyond the one its consumer identity names.
func Register(resourcePluginRegistry *resource.ResourceRegistry,
	digestProcessorRegistry *digestprocessor.RepositoryRegistry,
	httpConfig *httpv1alpha1.Config,
	credentialResolver credentials.Resolver,
) error {
	httpClient := httpclient.New(httpclient.WithConfig(httpConfig))
	opts := []githubrepository.Option{
		githubrepository.WithHTTPClient(httpClient),
		githubrepository.WithCredentialResolver(credentialResolver),
	}

	if err := resourcePluginRegistry.RegisterInternalResourcePlugin(githubrepository.NewResourceRepository(opts...)); err != nil {
		return fmt.Errorf("could not register github resource repository plugin: %w", err)
	}
	if err := digestProcessorRegistry.RegisterInternalDigestProcessorPlugin(githubdigest.NewDigestProcessor(opts...)); err != nil {
		return fmt.Errorf("could not register github digest processor plugin: %w", err)
	}

	return nil
}
