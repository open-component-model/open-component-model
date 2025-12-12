package oci

import (
	"errors"
	"log/slog"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/oci/cache/inmemory"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	"ocm.software/open-component-model/bindings/go/oci/transformer"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/blobtransformer"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentlister"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
)

func Register(
	compverRegistry *componentversionrepository.RepositoryRegistry,
	resRegistry *resource.ResourceRegistry,
	digRegistry *digestprocessor.RepositoryRegistry,
	blobTransformerRegistry *blobtransformer.Registry,
	compListRegistry *componentlister.ComponentListerRegistry,
	filesystemConfig *filesystemv1alpha1.Config,
	logger *slog.Logger,
) error {
	CachingComponentVersionRepositoryProvider := provider.NewComponentVersionRepositoryProvider(provider.WithUserAgent(Creator), provider.WithTempDir(filesystemConfig.TempFolder))

	resourceRepoPlugin := ResourceRepositoryPlugin{manifests: inmemory.New(), layers: inmemory.New(), filesystemConfig: filesystemConfig}
	ociBlobTransformerPlugin := transformer.New(logger)

	return errors.Join(
		compverRegistry.RegisterInternalComponentVersionRepositoryPlugin(
			CachingComponentVersionRepositoryProvider,
		),
		resRegistry.RegisterInternalResourcePlugin(
			&resourceRepoPlugin,
		),
		digRegistry.RegisterInternalDigestProcessorPlugin(
			&resourceRepoPlugin,
		),
		blobTransformerRegistry.RegisterInternalBlobTransformerPlugin(
			ociBlobTransformerPlugin,
		),
		compListRegistry.RegisterInternalComponentListerPlugin(
			&CTFComponentListerPlugin{},
		),
	)
}
