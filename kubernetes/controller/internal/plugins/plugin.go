// Package plugins provides initialization functions for OCM components in the controller.
package plugins

import (
	"errors"

	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
	"ocm.software/open-component-model/bindings/go/rsa/signing/handler"
	"ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

func Register(pm *manager.PluginManager, scheme *ocmruntime.Scheme) error {
	repositoryProvider := provider.NewComponentVersionRepositoryProvider()

	// Signing Plugin
	if err := scheme.RegisterScheme(v1alpha1.Scheme); err != nil {
		return err
	}

	signingHandler, err := handler.New(true)
	if err != nil {
		return err
	}

	return errors.Join(
		componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(
			scheme,
			pm.ComponentVersionRepositoryRegistry,
			repositoryProvider,
			&ociv1.Repository{},
		),
		componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(
			scheme,
			pm.ComponentVersionRepositoryRegistry,
			repositoryProvider,
			&ctfv1.Repository{},
		),
		signinghandler.RegisterInternalComponentSignatureHandler(
			scheme,
			pm.SigningRegistry,
			signingHandler,
			&v1alpha1.Config{},
		),

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
	)
}
