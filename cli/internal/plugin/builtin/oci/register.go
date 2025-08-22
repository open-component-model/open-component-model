package plugin

import (
	"errors"
	"log/slog"

	extractspecv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/extract/v1alpha1/spec"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/oci/cache/inmemory"
	access "ocm.software/open-component-model/bindings/go/oci/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/oci/transformer"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/blobtransformer"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Register(
	compverRegistry *componentversionrepository.RepositoryRegistry,
	resRegistry *resource.ResourceRegistry,
	digRegistry *digestprocessor.RepositoryRegistry,
	blobTransformerRegistry *blobtransformer.Registry,
	filesystemConfig *filesystemv1alpha1.Config,
	logger *slog.Logger,
) error {
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	access.MustAddToScheme(scheme)

	manifests := inmemory.New()
	layers := inmemory.New()

	cvRepoPlugin := ComponentVersionRepositoryPlugin{manifests: manifests, layers: layers, filesystemConfig: filesystemConfig}
	resourceRepoPlugin := ResourceRepositoryPlugin{scheme: scheme, manifests: manifests, layers: layers, filesystemConfig: filesystemConfig}
	ociBlobTransformerPlugin := transformer.New(logger)

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
		blobtransformer.RegisterInternalBlobTransformerPlugin(
			extractspecv1alpha1.Scheme,
			blobTransformerRegistry,
			ociBlobTransformerPlugin,
			&extractspecv1alpha1.Config{},
		),
	)
}
