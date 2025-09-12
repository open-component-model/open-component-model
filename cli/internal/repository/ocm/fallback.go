package ocm

import (
	"context"
	"fmt"
	"math"

	resolverruntime "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/runtime"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	fallback "ocm.software/open-component-model/bindings/go/repository/component/fallback/v1"

	//nolint:staticcheck // no replacement for resolvers available yet (https://github.com/open-component-model/ocm-project/issues/575)
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

// NewFromRefWithFallbackRepo creates a new ComponentRepository instance for the given component reference.
// It resolves the appropriate plugin and credentials for the repository.
// Deprecated: NewFromRefWithFallbackRepo is using the fallback repository which is deprecated.
//
//nolint:staticcheck // no replacement for resolvers available yet (https://github.com/open-component-model/ocm-project/issues/575)
func NewFromRefWithFallbackRepo(ctx context.Context, manager *manager.PluginManager, graph *credentials.Graph, resolvers []resolverruntime.Resolver, componentReference string) (*ComponentRepository, error) {
	ref, err := compref.Parse(componentReference)
	if err != nil {
		return nil, fmt.Errorf("parsing component reference %q failed: %w", componentReference, err)
	}
	if len(resolvers) == 0 {
		//nolint:staticcheck // no replacement for resolvers available yet (https://github.com/open-component-model/ocm-project/issues/575)
		resolvers = make([]resolverruntime.Resolver, 0)
	}

	if ref.Repository != nil {
		//nolint:staticcheck // no replacement for resolvers available yet (https://github.com/open-component-model/ocm-project/issues/575)
		resolvers = append(resolvers, resolverruntime.Resolver{
			Repository: ref.Repository,
			// Add the current repository as a resolver with the highest possible
			// priority.
			Priority: math.MaxInt,
		})
	}
	//nolint:staticcheck // no replacement for resolvers available yet (https://github.com/open-component-model/ocm-project/issues/575)
	res := make([]*resolverruntime.Resolver, 0, len(resolvers))
	for _, r := range resolvers {
		res = append(res, &r)
	}
	//nolint:staticcheck // no replacement for resolvers available yet (https://github.com/open-component-model/ocm-project/issues/575)
	fallbackRepo, err := fallback.NewFallbackRepository(ctx, provider.NewComponentVersionRepositoryProvider(), graph, res)
	if err != nil {
		return nil, fmt.Errorf("creating fallback repository failed: %w", err)
	}
	return &ComponentRepository{
		ref:  ref,
		base: fallbackRepo,
	}, nil
}
