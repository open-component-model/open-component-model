package pypi

import (
	"fmt"

	httpclient "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
	pypidigest "ocm.software/open-component-model/bindings/go/pypi/digest"
	pypiresource "ocm.software/open-component-model/bindings/go/pypi/repository/resource"
	pypicreds "ocm.software/open-component-model/bindings/go/pypi/spec/credentials"
)

// Register wires the PyPI resource repository, its digest processor and its
// credential scheme into the CLI plugin registries. Both the repository and
// the digest processor share one HTTP client built from httpConfig, so
// timeouts, retries and TLS settings apply to every PyPI request.
func Register(resourcePluginRegistry *resource.ResourceRegistry,
	digestProcessorRegistry *digestprocessor.RepositoryRegistry,
	credentialRepository *credentialrepository.RepositoryRegistry,
	httpConfig *httpv1alpha1.Config,
) error {
	httpClient := httpclient.New(httpclient.WithConfig(httpConfig))

	credentialRepository.Register(pypicreds.Scheme)

	repository := pypiresource.NewResourceRepository(pypiresource.WithHTTPClient(httpClient))
	if err := resourcePluginRegistry.RegisterInternalResourcePlugin(repository); err != nil {
		return fmt.Errorf("could not register pypi resource repository plugin: %w", err)
	}

	digestProcessor := pypidigest.NewDigestProcessor(pypiresource.WithHTTPClient(httpClient))
	if err := digestProcessorRegistry.RegisterInternalDigestProcessorPlugin(digestProcessor); err != nil {
		return fmt.Errorf("could not register pypi digest processor plugin: %w", err)
	}
	return nil
}
