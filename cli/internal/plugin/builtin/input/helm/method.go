package helm

import (
	"fmt"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	helminput "ocm.software/open-component-model/bindings/go/helm/input"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
)

const creator = "Builtin Helm Input Plugin"

func Register(inputRegistry *input.RepositoryRegistry, filesystemConfig *filesystemv1alpha1.Config, httpConfig *httpv1alpha1.Config) error {
	httpClient := ocmhttp.New(
		ocmhttp.WithConfig(httpConfig),
		ocmhttp.WithUserAgent(creator),
	)

	method := &helminput.InputMethod{
		TempFolder: filesystemConfig.TempFolder,
		HTTPClient: httpClient,
	}

	if err := inputRegistry.RegisterInternalResourceInputPlugin(method); err != nil {
		return fmt.Errorf("could not register helm resource input method: %w", err)
	}

	return nil
}
