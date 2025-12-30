package ocm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	pathmatcher "ocm.software/open-component-model/bindings/go/repository/component/pathmatcher/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// resolverProvider provides a [repository.ComponentVersionRepository] based on a set of path matcher resolvers.
// It uses the [manager.PluginManager] to access the [repository.ComponentVersionRepository] and a
// [credentials.Resolver] to resolve credentials for the repository.
type resolverProvider struct {
	// repoProvider is the repository.ComponentVersionRepositoryForComponentProvider used to
	// get the repositories based on the repository specs in the resolvers.
	repoProvider repository.ComponentVersionRepositoryProvider
	// graph is the [credentials.Resolver] used to resolve credentials for the repository.
	// It can be nil, if no credential graph is available.
	graph credentials.Resolver
	// provider is the [pathmatcher.SpecProvider] used to get the repository spec for a given identity.
	// It is configured with a set of path matcher resolvers.
	provider *pathmatcher.SpecProvider
}

// GetComponentVersionRepositoryForComponent returns a [repository.ComponentVersionRepository] based on the path matcher resolvers.
// It resolves any necessary credentials using the credential graph if available.
func (r *resolverProvider) GetComponentVersionRepositoryForComponent(ctx context.Context, component, version string) (repository.ComponentVersionRepository, error) {
	repoSpec, err := r.provider.GetRepositorySpec(ctx, runtime.Identity{
		descruntime.IdentityAttributeName:    component,
		descruntime.IdentityAttributeVersion: version,
	})
	if err != nil {
		return nil, fmt.Errorf("getting repository spec for component %s:%s failed: %w", component, version, err)
	}

	var credMap map[string]string
	consumerIdentity, err := r.repoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, repoSpec)
	if err == nil {
		if r.graph != nil {
			if credMap, err = r.graph.Resolve(ctx, consumerIdentity); err != nil {
				if errors.Is(err, credentials.ErrNotFound) {
					slog.DebugContext(ctx, fmt.Sprintf("resolving credentials for repository %q failed: %s", repoSpec, err.Error()))
				} else {
					return nil, fmt.Errorf("resolving credentials for repository %q failed: %w", repoSpec, err)
				}
			}
		}
	} else {
		slog.DebugContext(ctx, "could not get credential consumer identity for component version repository", "repository", repoSpec, "error", err)
	}

	repo, err := r.repoProvider.GetComponentVersionRepository(ctx, repoSpec, credMap)
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
