package plugin

import (
	"errors"

	"ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci/cache/inmemory"
	access "ocm.software/open-component-model/bindings/go/oci/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Register(
	compverRegistry *componentversionrepository.RepositoryRegistry,
	resRegistry *resource.ResourceRegistry,
	digRegistry *digestprocessor.RepositoryRegistry,
	filesystemConfig *v1alpha1.Config,
) error {
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	access.MustAddToScheme(scheme)

	manifests := inmemory.New()
	layers := inmemory.New()

	cvRepoPlugin := ComponentVersionRepositoryPlugin{manifests: manifests, layers: layers, filesystemConfig: filesystemConfig}
	resourceRepoPlugin := ResourceRepositoryPlugin{scheme: scheme, manifests: manifests, layers: layers, filesystemConfig: filesystemConfig}

	return errors.Join(
		componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(
			scheme,
			compverRegistry,
			&cvRepoPlugin,
			&ociv1.Repository{},
		),
		resource.RegisterInternalResourcePlugin(
			scheme,
			resRegistry,
			&resourceRepoPlugin,
			&v1.OCIImage{},
		),
		digestprocessor.RegisterInternalDigestProcessorPlugin(
			scheme,
			digRegistry,
			&resourceRepoPlugin,
			&v1.OCIImage{},
		),
	)
}
