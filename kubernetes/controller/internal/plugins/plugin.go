// Package plugins provides initialization functions for OCM components in the controller.
package plugins

import (
	"ocm.software/open-component-model/bindings/go/credentials"
	ocicredentials "ocm.software/open-component-model/bindings/go/oci/credentials"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ocicredentialsspec "ocm.software/open-component-model/bindings/go/oci/spec/credentials"
	ocicredentialsspecv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
	"ocm.software/open-component-model/bindings/go/rsa/signing/handler"
	signingv1alpha1 "ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Register(pm *manager.PluginManager) error {
	// Repository Plugin
	repositoryScheme := runtime.NewScheme()

	// TODO: Remove when RegisterWithAlias is fixed
	//       https://github.com/open-component-model/open-component-model/pull/1284
	repositoryScheme.MustRegisterWithAlias(&ociv1.Repository{},
		runtime.NewVersionedType(ociv1.Type, ociv1.Version),
		runtime.NewUnversionedType(ociv1.Type),
		runtime.NewVersionedType(ociv1.ShortType, ociv1.Version),
		runtime.NewUnversionedType(ociv1.ShortType),
		runtime.NewVersionedType(ociv1.ShortType2, ociv1.Version),
		runtime.NewUnversionedType(ociv1.ShortType2),
		runtime.NewVersionedType(ociv1.LegacyRegistryType, ociv1.Version),
		runtime.NewUnversionedType(ociv1.LegacyRegistryType),
		runtime.NewVersionedType(ociv1.LegacyRegistryType2, ociv1.Version),
		runtime.NewUnversionedType(ociv1.LegacyRegistryType2),
	)

	repositoryScheme.MustRegisterWithAlias(&ctfv1.Repository{},
		runtime.NewVersionedType(ctfv1.Type, ctfv1.Version),
		runtime.NewUnversionedType(ctfv1.Type),
		runtime.NewVersionedType(ctfv1.ShortType, ctfv1.Version),
		runtime.NewUnversionedType(ctfv1.ShortType),
		runtime.NewVersionedType(ctfv1.ShortType2, ctfv1.Version),
		runtime.NewUnversionedType(ctfv1.ShortType2),
	)

	repositoryProvider := provider.NewComponentVersionRepositoryProvider(provider.WithScheme(repositoryScheme))

	if err := componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(
		repositoryScheme,
		pm.ComponentVersionRepositoryRegistry,
		repositoryProvider,
		&ociv1.Repository{},
	); err != nil {
		return err
	}

	if err := componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(
		repositoryScheme,
		pm.ComponentVersionRepositoryRegistry,
		repositoryProvider,
		&ctfv1.Repository{},
	); err != nil {
		return err
	}

	// Credential Plugin
	credScheme := runtime.NewScheme()
	credScheme.MustRegisterWithAlias(
		&ocicredentialsspecv1.DockerConfig{},
		ocicredentialsspec.CredentialRepositoryConfigType,                                  // DockerConfig/v1
		runtime.NewUnversionedType(ocicredentialsspec.CredentialRepositoryConfigType.Name), // DockerConfig
	)

	if err := credentialrepository.RegisterInternalCredentialRepositoryPlugin(
		credScheme,
		pm.CredentialRepositoryRegistry,
		&ocicredentials.OCICredentialRepository{},
		&ocicredentialsspecv1.DockerConfig{},
		[]runtime.Type{credentials.AnyConsumerIdentityType},
	); err != nil {
		return err
	}

	// Signing Plugin
	signingScheme := runtime.NewScheme()
	if err := signingScheme.RegisterScheme(signingv1alpha1.Scheme); err != nil {
		return err
	}

	signingHandler, err := handler.New(true)
	if err != nil {
		return err
	}

	if err := signinghandler.RegisterInternalComponentSignatureHandler(
		signingScheme,
		pm.SigningRegistry,
		signingHandler,
		&signingv1alpha1.Config{},
	); err != nil {
		return err
	}

	return nil
}
