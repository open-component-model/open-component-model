package maven

import (
	"fmt"

	httpclient "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	mavendigest "ocm.software/open-component-model/bindings/go/maven/digest"
	mavenresource "ocm.software/open-component-model/bindings/go/maven/repository/resource"
	mavencreds "ocm.software/open-component-model/bindings/go/maven/spec/credentials"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
)

// Register wires the Maven resource repository, its digest processor and its
// credential scheme into the CLI plugin registries. Both the repository and
// the digest processor share one HTTP client built from httpConfig, so
// timeouts, retries and TLS settings apply to every Maven request.
func Register(resourcePluginRegistry *resource.ResourceRegistry,
	digestProcessorRegistry *digestprocessor.RepositoryRegistry,
	credentialRepository *credentialrepository.RepositoryRegistry,
	httpConfig *httpv1alpha1.Config,
) error {
	httpClient := httpclient.New(httpclient.WithConfig(httpConfig))

	credentialRepository.Register(mavencreds.Scheme)

	repository := mavenresource.NewResourceRepository(mavenresource.WithHTTPClient(httpClient))
	if err := resourcePluginRegistry.RegisterInternalResourcePlugin(repository); err != nil {
		return fmt.Errorf("could not register maven resource repository plugin: %w", err)
	}

	digestProcessor := mavendigest.NewDigestProcessor(mavenresource.WithHTTPClient(httpClient))
	if err := digestProcessorRegistry.RegisterInternalDigestProcessorPlugin(digestProcessor); err != nil {
		return fmt.Errorf("could not register maven digest processor plugin: %w", err)
	}
	return nil
}
