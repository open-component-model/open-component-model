package helm

import (
	"fmt"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	helminput "ocm.software/open-component-model/bindings/go/helm/input"
	helm "ocm.software/open-component-model/bindings/go/helm/spec/credentials"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
)

func Register(inputRegistry *input.RepositoryRegistry,
	repositoryRegistry *credentialrepository.RepositoryRegistry,
	filesystemConfig *filesystemv1alpha1.Config,
	httpConfig *httpv1alpha1.Config,
) error {
	method := &helminput.InputMethod{
		TempFolder: filesystemConfig.TempFolder,
		HTTPConfig: httpConfig,
	}

	repositoryRegistry.Register(helm.Scheme)

	if err := inputRegistry.RegisterInternalResourceInputPlugin(method); err != nil {
		return fmt.Errorf("could not register helm resource input method: %w", err)
	}

	return nil
}
