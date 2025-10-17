package ocm

import (
	"context"
	"fmt"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	resolverruntime "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/runtime"
	resolverv1 "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository"
	//nolint:staticcheck // kept for backward compatibility, use resolvers instead
	fallback "ocm.software/open-component-model/bindings/go/repository/component/fallback/v1"
)

// fallbackProvider provides a [repository.ComponentVersionRepository] based on a set of fallback resolvers.
// This is a deprecated mechanism and will be replaced by path matcher based resolvers in the future.
// It uses the [manager.PluginManager] to access the [repository.ComponentVersionRepository] and a
// [credentials.GraphResolver] to resolve credentials for the repository.
// This implementation is solely provided to support backward compatibility for existing configurations.
type fallbackProvider struct {
	// manager is the [manager.PluginManager] used to access the [repository.ComponentVersionRepository].
	manager *manager.PluginManager
	// graph is the [credentials.GraphResolver] used to resolve credentials for the repository.
	// It can be nil, if no credential graph is available.
	graph credentials.GraphResolver
	// resolvers is the list of [resolverruntime.Resolver] used to access the [repository.ComponentVersionRepository].
	//nolint:staticcheck // kept for backward compatibility, use resolvers instead
	resolvers []*resolverruntime.Resolver
}

// newFromConfigWithFallback creates a new fallbackProvider based on the provided configuration.
// It uses the provided PluginManager to access the [repository.ComponentVersionRepository].
// It uses the provided [credentials.GraphResolver] to resolve credentials for the repository.
// The configuration is expected to contain a list of fallback resolvers.
// This implementation is solely provided to support backward compatibility for existing configurations.
func newFromConfigWithFallback(
	manager *manager.PluginManager,
	graph credentials.GraphResolver,
	//nolint:staticcheck // kept for backward compatibility, use resolvers instead
	resolvers []*resolverruntime.Resolver,
) *fallbackProvider {
	return &fallbackProvider{
		manager:   manager,
		graph:     graph,
		resolvers: resolvers,
	}
}

// GetComponentVersionRepository returns a [repository.ComponentVersionRepository] based on the fallback resolvers.
// It uses the [manager.PluginManager] to access the [repository.ComponentVersionRepository].
// It uses the [credentials.GraphResolver] to resolve credentials for the repository.
// This implementation is solely provided to support backward compatibility for existing configurations.
//
//nolint:staticcheck // kept for backward compatibility, use resolvers instead
func (f *fallbackProvider) GetComponentVersionRepository(ctx context.Context, _, _ string) (repository.ComponentVersionRepository, error) {
	//nolint:staticcheck // no replacement for resolvers available yet https://github.com/open-component-model/ocm-project/issues/575
	fallbackRepo, err := fallback.NewFallbackRepository(ctx, f.manager.ComponentVersionRepositoryRegistry, f.graph, f.resolvers)
	if err != nil {
		return nil, fmt.Errorf("creating fallback repository failed: %w", err)
	}

	return fallbackRepo, nil
}

// FallbackResolversFromConfig extracts fallback resolvers from the provided configuration.
// It filters the configuration for resolver configurations, merges them, and converts them to runtime format.
// Returns a slice of resolvers or an error if the process fails.
// This implementation is solely provided to support backward compatibility for existing configurations.
//
//nolint:staticcheck // kept for backward compatibility, use resolvers instead
func FallbackResolversFromConfig(config *genericv1.Config) ([]*resolverruntime.Resolver, error) {
	filtered, err := genericv1.FilterForType[*resolverv1.Config](resolverv1.Scheme, config)
	if err != nil {
		return nil, fmt.Errorf("filtering configuration for resolver config failed: %w", err)
	}
	resolverConfigV1 := resolverv1.Merge(filtered...)

	resolverConfig, err := resolverruntime.ConvertFromV1(ocirepository.Scheme, resolverConfigV1)
	if err != nil {
		return nil, fmt.Errorf("converting resolver configuration from v1 to runtime failed: %w", err)
	}
	var resolvers []*resolverruntime.Resolver
	if resolverConfig != nil && len(resolverConfig.Resolvers) > 0 {
		resolvers = make([]*resolverruntime.Resolver, len(resolverConfig.Resolvers))
		for index, resolver := range resolverConfig.Resolvers {
			resolvers[index] = &resolver
		}
	}
	return resolvers, nil
}
