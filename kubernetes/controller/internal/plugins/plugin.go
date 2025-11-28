// Package plugins provides initialization functions for OCM components in the controller.
package plugins

import (
	"ocm.software/open-component-model/bindings/go/credentials"
	ocicredentials "ocm.software/open-component-model/bindings/go/oci/credentials"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ocicredentialsspec "ocm.software/open-component-model/bindings/go/oci/spec/credentials"
	ocicredentialsspecv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	ocmrepository "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
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
	// TODO: Replace global variable when options are supported in repository providers
	//       https://github.com/open-component-model/open-component-model/pull/1282
	ocmrepository.Scheme = runtime.NewScheme()

	// TODO: Remove when RegisterWithAlias is fixed
	ocmrepository.Scheme.MustRegisterWithAlias(&oci.Repository{},
		runtime.NewVersionedType(oci.Type, oci.Version),
		runtime.NewUnversionedType(oci.Type),
		runtime.NewVersionedType(oci.ShortType, oci.Version),
		runtime.NewUnversionedType(oci.ShortType),
		runtime.NewVersionedType(oci.ShortType2, oci.Version),
		runtime.NewUnversionedType(oci.ShortType2),
		runtime.NewVersionedType(oci.LegacyRegistryType, oci.Version),
		runtime.NewUnversionedType(oci.LegacyRegistryType),
		runtime.NewVersionedType(oci.LegacyRegistryType2, oci.Version),
		runtime.NewUnversionedType(oci.LegacyRegistryType2),
	)

	ocmrepository.Scheme.MustRegisterWithAlias(&ctf.Repository{},
		runtime.NewVersionedType(ctf.Type, ctf.Version),
		runtime.NewUnversionedType(ctf.Type),
		runtime.NewVersionedType(ctf.ShortType, ctf.Version),
		runtime.NewUnversionedType(ctf.ShortType),
		runtime.NewVersionedType(ctf.ShortType2, ctf.Version),
		runtime.NewUnversionedType(ctf.ShortType2),
	)

	repositoryProvider := provider.NewComponentVersionRepositoryProvider()

	if err := componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(
		ocmrepository.Scheme,
		pm.ComponentVersionRepositoryRegistry,
		repositoryProvider,
		&ociv1.Repository{},
	); err != nil {
		return err
	}

	if err := componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(
		ocmrepository.Scheme,
		pm.ComponentVersionRepositoryRegistry,
		repositoryProvider,
		&ctfv1.Repository{},
	); err != nil {
		return err
	}

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

	// TODO: Enable these plugins when needed
	//  resource.RegisterInternalResourcePlugin(
	//  	scheme,
	//  	pm.ResourcePluginRegistry,
	//  	&resourceRepoPlugin,
	//  	&v1.OCIImage{},
	//  ),
	//  digestprocessor.RegisterInternalDigestProcessorPlugin(
	//  	scheme,
	//  	digRegistry,
	//  	&resourceRepoPlugin,
	//  	&v1.OCIImage{},
	//  ),
	//  blobtransformer.RegisterInternalBlobTransformerPlugin(
	//  	extractspecv1alpha1.Scheme,
	//  	blobTransformerRegistry,
	//  	ociBlobTransformerPlugin,
	//  	&extractspecv1alpha1.Config{},
	//  ),
	//  componentlister.RegisterInternalComponentListerPlugin(
	//  	scheme,
	//  	compListRegistry,
	//  	&CTFComponentListerPlugin{},
	//  	&ctfv1.Repository{},
	//  ),

	return nil
}
