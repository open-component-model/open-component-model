package plugin

import (
	"errors"

	"ocm.software/open-component-model/bindings/go/oci/cache/inmemory"
	access "ocm.software/open-component-model/bindings/go/oci/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Register(
	compverRegistry *componentversionrepository.RepositoryRegistry,
	resRegistry *resource.RepositoryRegistry,
) error {
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	access.MustAddToScheme(scheme)

	memory := inmemory.New()

	repoCache := newRepoCache()

	cvRepoPlugin := ComponentVersionRepositoryPlugin{scheme: scheme, memory: memory, repoCache: repoCache}
	resourceRepoPlugin := ResourceRepositoryPlugin{scheme: scheme, memory: memory, repoCache: repoCache}

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
	)
}
