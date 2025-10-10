package ocm

import (
	"context"
	"fmt"
	"log/slog"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository"
	pathmatcher "ocm.software/open-component-model/bindings/go/repository/component/pathmatcher/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type resolverProvider struct {
	manager   *manager.PluginManager
	graph     credentials.GraphResolver
	resolvers []*resolverspec.Resolver
	provider  *pathmatcher.SpecProvider
}

// TODO
func newFromConfigWithPathMatcher(
	ctx context.Context,
	manager *manager.PluginManager,
	graph credentials.GraphResolver,
	resolvers []*resolverspec.Resolver,
) (*resolverProvider, error) {
	if len(resolvers) == 0 {
		return nil, fmt.Errorf("no resolvers configured")
	}

	// TODO: add a fallback entry with wildcard * which is being injected by constructor as resolver
	// set as MIN PRIO in this case
	provider := pathmatcher.NewSpecProvider(ctx, resolvers)

	return &resolverProvider{
		manager:   manager,
		graph:     graph,
		resolvers: resolvers,
		provider:  provider,
	}, nil
}

func (r *resolverProvider) GetComponentVersionRepository(ctx context.Context, identity runtime.Identity) (repository.ComponentVersionRepository, error) {
	repoSpec, err := r.provider.GetRepositorySpec(ctx, identity)
	if err != nil {
		return nil, fmt.Errorf("getting repository spec for identity %q failed: %w", identity, err)
	}

	var credMap map[string]string
	consumerIdentity, err := r.manager.ComponentVersionRepositoryRegistry.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, repoSpec)
	if err == nil {
		if r.graph != nil {
			if credMap, err = r.graph.Resolve(ctx, consumerIdentity); err != nil {
				slog.DebugContext(ctx, fmt.Sprintf("resolving credentials for repository %q failed: %s", repoSpec, err.Error()))
			}
		}
	} else {
		slog.WarnContext(ctx, "could not get credential consumer identity for component version repository", "repository", repoSpec, "error", err)
	}

	repo, err := r.manager.ComponentVersionRepositoryRegistry.GetComponentVersionRepository(ctx, repoSpec, credMap)
	if err != nil {
		return nil, fmt.Errorf("getting component version repository for %q failed: %w", repoSpec, err)
	}

	return repo, nil
}

func ResolversFromConfig(config *genericv1.Config) ([]*resolverspec.Resolver, error) {
	filtered, err := genericv1.FilterForType[*resolverspec.Config](resolverspec.Scheme, config)
	if err != nil {
		return nil, fmt.Errorf("filtering configuration for resolver config failed: %w", err)
	}

	result := make([]*resolverspec.Resolver, 0, len(filtered))
	for _, r := range filtered {
		result = append(result, r.Resolvers...)
	}

	return result, nil
}
