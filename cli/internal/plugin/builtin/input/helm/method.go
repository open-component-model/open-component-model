package helm

import (
	"fmt"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	helminput "ocm.software/open-component-model/bindings/go/helm/input"
	helmidentityv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/identity/v1"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/consumeridentitytype"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialtype"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Register(
	inputRegistry *input.RepositoryRegistry,
	credentialTypeRegistry *credentialtype.Registry,
	identityTypeRegistry *consumeridentitytype.Registry,
	filesystemConfig *filesystemv1alpha1.Config,
) error {
	method := &helminput.InputMethod{
		TempFolder: filesystemConfig.TempFolder,
	}

	if err := inputRegistry.RegisterInternalResourceInputPlugin(method); err != nil {
		return fmt.Errorf("could not register helm resource input method: %w", err)
	}

	credentialTypeRegistry.MustRegister(&helmcredsv1.HelmHTTPCredentials{},
		runtime.NewVersionedType(helmcredsv1.HelmHTTPCredentialsType, helmcredsv1.Version),
	)
	identityTypeRegistry.MustRegister(&helmidentityv1.HelmChartRepositoryIdentity{},
		helmidentityv1.VersionedType,
		helmidentityv1.Type,
	)

	return nil
}
