package plugin

import (
	"errors"
	"fmt"
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
	builtinv1 "ocm.software/open-component-model/cli/internal/plugin/builtin/config/v1"
)

func Register(
	compverRegistry *componentversionrepository.RepositoryRegistry,
	resRegistry *resource.ResourceRegistry,
	digRegistry *digestprocessor.RepositoryRegistry,
	config *builtinv1.BuiltinPluginConfig,
	logger *slog.Logger,
) error {
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	access.MustAddToScheme(scheme)

	manifests := inmemory.New()
	layers := inmemory.New()

	cvRepoPlugin := ComponentVersionRepositoryPlugin{scheme: scheme, manifests: manifests, layers: layers}
	resourceRepoPlugin := ResourceRepositoryPlugin{scheme: scheme, manifests: manifests, layers: layers}

	// Configure plugins if configuration is provided
	if config != nil {
		if err := cvRepoPlugin.Configure(config, logger); err != nil {
			return fmt.Errorf("failed to configure OCI component version repository plugin: %w", err)
		}
		if err := resourceRepoPlugin.Configure(config, logger); err != nil {
			return fmt.Errorf("failed to configure OCI resource repository plugin: %w", err)
		}
	}

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
