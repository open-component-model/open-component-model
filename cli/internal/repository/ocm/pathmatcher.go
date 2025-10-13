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

// resolverProvider provides a [repository.ComponentVersionRepository] based on a set of path matcher resolvers.
// It uses the [manager.PluginManager] to access the [repository.ComponentVersionRepository] and a
// [credentials.GraphResolver] to resolve credentials for the repository.
type resolverProvider struct {
	// manager is the [manager.PluginManager] used to access the [repository.ComponentVersionRepository].
	manager *manager.PluginManager
	// graph is the [credentials.GraphResolver] used to resolve credentials for the repository.
	// It can be nil, if no credential graph is available.
	graph credentials.GraphResolver
	// provider is the [pathmatcher.SpecProvider] used to get the repository spec for a given identity.
	// It is configured with a set of path matcher resolvers.
	provider *pathmatcher.SpecProvider
}

// newFromConfigWithPathMatcher creates a new resolverProvider based on the provided configuration.
// It uses the provided PluginManager to access the [repository.ComponentVersionRepository].
// It uses the provided [credentials.GraphResolver] to resolve credentials for the repository.
// The configuration is expected to contain a list of path matcher resolvers.
// If no resolvers are configured, an error is returned.
func newFromConfigWithPathMatcher(
	ctx context.Context,
	manager *manager.PluginManager,
	graph credentials.GraphResolver,
	resolvers []*resolverspec.Resolver,
) (*resolverProvider, error) {
	if len(resolvers) == 0 {
		return nil, fmt.Errorf("no resolvers configured")
	}

	provider := pathmatcher.NewSpecProvider(ctx, resolvers)

	return &resolverProvider{
		manager:  manager,
		graph:    graph,
		provider: provider,
	}, nil
}

// GetComponentVersionRepository returns a [repository.ComponentVersionRepository] based on the path matcher resolvers.
// It resolves any necessary credentials using the credential graph if available.
// It uses the [manager.PluginManager] to access the [repository.ComponentVersionRepository].
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

// ResolversFromConfig extracts a list of resolvers from a generic configuration.
// It filters the configuration for entries of type [resolverspec.Config] and aggregates
// all resolvers defined in these entries into a single list.
// If the filtering process fails, an error is returned.
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
