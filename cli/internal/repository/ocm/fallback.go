package ocm

import (
	"context"
	"fmt"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	resolverruntime "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/runtime"
	resolverv1 "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/repository"
	fallback "ocm.software/open-component-model/bindings/go/repository/component/fallback/v1"
)

// fallbackProvider provides a [repository.ComponentVersionRepository] based on a set of fallback resolvers.
// This is a deprecated mechanism and will be replaced by path matcher based resolvers in the future.
// This implementation is solely provided to support backward compatibility for existing configurations.
type fallbackProvider struct {
	// repoProvider is the repository.ComponentVersionRepositoryForComponentProvider used to
	// get the repositories based on the repository specs in the resolvers.
	repoProvider repository.ComponentVersionRepositoryProvider
	// graph is the [credentials.Resolver] used to resolve credentials for the repository.
	// It can be nil, if no credential graph is available.
	graph credentials.Resolver
	// resolvers is the list of [resolverruntime.Resolver] used to access the [repository.ComponentVersionRepository].
	resolvers []*resolverruntime.Resolver
}

// GetComponentVersionRepositoryForComponent returns a [repository.ComponentVersionRepository] based on the fallback resolvers.
// This implementation is solely provided to support backward compatibility for existing configurations.
func (f *fallbackProvider) GetComponentVersionRepositoryForComponent(ctx context.Context, component, version string) (repository.ComponentVersionRepository, error) {
	fallbackRepo, err := fallback.NewFallbackRepository(ctx, f.repoProvider, f.graph, f.resolvers)
	if err != nil {
		return nil, fmt.Errorf("creating fallback repository failed: %w", err)
	}

	return fallbackRepo, nil
}

// FallbackResolversFromConfig extracts fallback resolvers from the provided configuration.
// It filters the configuration for resolver configurations, merges them, and converts them to runtime format.
// Returns a slice of resolvers or an error if the process fails.
// This implementation is solely provided to support backward compatibility for existing configurations.
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
