package ocm

import (
	"context"
	"encoding/json"
	"fmt"

	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	resolver "ocm.software/open-component-model/bindings/go/repository/component/resolver/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

// NewFromRefWithResolverRepo creates a new ComponentRepository instance for the given component reference.
// It resolves the appropriate plugin and credentials for the repository.
func NewFromRefWithResolverRepo(ctx context.Context, manager *manager.PluginManager, graph *credentials.Graph, resolvers []*resolverspec.Resolver, componentReference string) (*ComponentRepository, error) {
	ref, err := compref.Parse(componentReference)
	if err != nil {
		return nil, fmt.Errorf("parsing component reference %q failed: %w", componentReference, err)
	}
	if len(resolvers) == 0 {
		resolvers = make([]*resolverspec.Resolver, 0)
	}

	if ref.Repository != nil {
		j, err := json.Marshal(ref.Repository)
		if err != nil {
			return nil, fmt.Errorf("marshaling repository config failed: %w", err)
		}
		raw := &runtime.Raw{
			Type: ref.Repository.GetType(),
			Data: j,
		}
		resolvers = append(resolvers, &resolverspec.Resolver{
			Repository:    raw,
			ComponentName: componentReference,
		})
	}

	res := make([]*resolverspec.Resolver, 0, len(resolvers))
	for _, r := range resolvers {
		res = append(res, r)
	}

	resolverRepo, err := resolver.NewResolverRepository(ctx, provider.NewComponentVersionRepositoryProvider(), graph, res)
	if err != nil {
		return nil, fmt.Errorf("creating resolver repository failed: %w", err)
	}

	return &ComponentRepository{
		ref:  ref,
		base: resolverRepo,
	}, nil
}
