package ocm

import (
	"context"
	"fmt"
	"math"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	resolverruntime "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/runtime"
	resolverv1 "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	fallback "ocm.software/open-component-model/bindings/go/repository/component/fallback/v1"

	//nolint:staticcheck // no replacement for resolvers available yet https://github.com/open-component-model/ocm-project/issues/575
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

// NewFromRefWithFallbackRepo creates a new ComponentRepository instance for the given component reference.
// It resolves the appropriate plugin and credentials for the repository.
//
//nolint:staticcheck // no replacement for resolvers available yet https://github.com/open-component-model/ocm-project/issues/575
func NewFromRefWithFallbackRepo(ctx context.Context, manager *manager.PluginManager, graph credentials.GraphResolver, resolvers []*resolverruntime.Resolver, componentReference string) (*ComponentRepository, error) {
	ref, err := compref.Parse(componentReference)
	if err != nil {
		return nil, fmt.Errorf("parsing component reference %q failed: %w", componentReference, err)
	}
	if len(resolvers) == 0 {
		//nolint:staticcheck // no replacement for resolvers available yet https://github.com/open-component-model/ocm-project/issues/575
		resolvers = make([]*resolverruntime.Resolver, 0)
	}

	if ref.Repository != nil {
		//nolint:staticcheck // no replacement for resolvers available yet https://github.com/open-component-model/ocm-project/issues/575
		resolvers = append(resolvers, &resolverruntime.Resolver{
			Repository: ref.Repository,
			// Add the current repository as a resolver with the highest possible
			// priority.
			Priority: math.MaxInt,
		})
	}

	//nolint:staticcheck // no replacement for resolvers available yet https://github.com/open-component-model/ocm-project/issues/575
	fallbackRepo, err := fallback.NewFallbackRepository(ctx, manager.ComponentVersionRepositoryRegistry, graph, resolvers)
	if err != nil {
		return nil, fmt.Errorf("creating fallback repository failed: %w", err)
	}
	return &ComponentRepository{
		ref:  ref,
		base: fallbackRepo,
	}, nil
}

//nolint:staticcheck // no replacement for resolvers available yet https://github.com/open-component-model/ocm-project/issues/575
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
