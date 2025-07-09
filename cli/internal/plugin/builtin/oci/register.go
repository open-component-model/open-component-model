package plugin

import (
	"errors"
	"log/slog"

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

func Register(compverRegistry *componentversionrepository.RepositoryRegistry, resRegistry *resource.ResourceRegistry, digRegistry *digestprocessor.RepositoryRegistry, logger *slog.Logger) error {
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	access.MustAddToScheme(scheme)

	manifests := inmemory.New()
	layers := inmemory.New()

	cvRepoPlugin := ComponentVersionRepositoryPlugin{scheme: scheme, manifests: manifests, layers: layers, logger: logger}
	resourceRepoPlugin := ResourceRepositoryPlugin{scheme: scheme, manifests: manifests, layers: layers, logger: logger}

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
