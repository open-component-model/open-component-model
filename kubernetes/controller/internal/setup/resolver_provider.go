package setup

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	pathmatcher "ocm.software/open-component-model/bindings/go/repository/component/pathmatcher/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ComponentVersionRepositoryForComponentProvider provides a [repository.ComponentVersionRepository] based on a given identity.
// Implementations may use different strategies to resolve the repository, such as using component references,
// configuration-based resolvers, or other mechanisms.
type ComponentVersionRepositoryForComponentProvider interface {
	// GetComponentVersionRepositoryForComponent returns a repository for the given component and version.
	// The repository is resolved based on the provider's configuration (e.g., pattern matching, fallback resolvers).
	GetComponentVersionRepositoryForComponent(ctx context.Context, component, version string) (repository.ComponentVersionRepository, error)

	// GetRepositorySpecForComponent returns the resolved repository specification for the given component and version.
	// It is used during cache key generation when the actual spec depends on resolver pattern matching.
	GetRepositorySpecForComponent(ctx context.Context, component, version string) (runtime.Typed, error)
}

// resolverProvider provides a [repository.ComponentVersionRepository] based on a set of path matcher resolvers.
// It uses path pattern matching leveraging the github.com/gobwas/glob library to match component names
// to determine which OCM repository specification to use for resolving component versions.
type resolverProvider struct {
	// repoProvider is the repository.ComponentVersionRepositoryProvider used to
	// get the repositories based on the repository specs in the resolvers.
	repoProvider repository.ComponentVersionRepositoryProvider
	// graph is the [credentials.Resolver] used to resolve credentials for the repository.
	// It can be nil, if no credential graph is available.
	graph credentials.Resolver
	// provider is the [pathmatcher.SpecProvider] used to get the repository spec for a given identity.
	// It is configured with a set of path matcher resolvers.
	provider *pathmatcher.SpecProvider
	// logger is used for logging resolver operations
	logger *logr.Logger
}

// GetRepositorySpecForComponent returns the resolved repository specification for the given component and version.
// It uses the path matcher resolvers to determine which repository spec applies.
func (r *resolverProvider) GetRepositorySpecForComponent(ctx context.Context, component, version string) (runtime.Typed, error) {
	return r.provider.GetRepositorySpec(ctx, runtime.Identity{
		descruntime.IdentityAttributeName:    component,
		descruntime.IdentityAttributeVersion: version,
	})
}

// GetComponentVersionRepositoryForComponent returns a [repository.ComponentVersionRepository] based on the path matcher resolvers.
// It resolves any necessary credentials using the credential graph if available.
func (r *resolverProvider) GetComponentVersionRepositoryForComponent(ctx context.Context, component, version string) (repository.ComponentVersionRepository, error) {
	repoSpec, err := r.GetRepositorySpecForComponent(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("getting repository spec for component %s:%s failed: %w", component, version, err)
	}

	var credMap map[string]string
	consumerIdentity, err := r.repoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, repoSpec)
	if err == nil {
		if r.graph != nil {
			if credMap, err = r.graph.Resolve(ctx, consumerIdentity); err != nil {
				if errors.Is(err, credentials.ErrNotFound) {
					if r.logger != nil {
						r.logger.V(1).Info("resolving credentials for repository failed",
							"repository", repoSpec,
							"error", err.Error())
					}
				} else {
					return nil, fmt.Errorf("resolving credentials for repository %q failed: %w", repoSpec, err)
				}
			}
		}
	} else {
		if r.logger != nil {
			r.logger.V(1).Info("could not get credential consumer identity for component version repository",
				"repository", repoSpec,
				"error", err.Error())
		}
	}

	repo, err := r.repoProvider.GetComponentVersionRepository(ctx, repoSpec, credMap)
	if err != nil {
		return nil, fmt.Errorf("getting component version repository for %q failed: %w", repoSpec, err)
	}

	return repo, nil
}

// GetResolversV1Alpha1 extracts a list of path matcher resolvers from a generic configuration.
// It filters the configuration for entries of type [resolverspec.Config] and aggregates
// all resolvers defined in these entries into a single list.
// If the filtering process fails, an error is returned.
func GetResolversV1Alpha1(config *genericv1.Config) ([]*resolverspec.Resolver, error) {
	if config == nil || len(config.Configurations) == 0 {
		return nil, nil
	}

	filtered, err := genericv1.FilterForType[*resolverspec.Config](resolverspec.Scheme, config)
	if err != nil {
		return nil, fmt.Errorf("filtering configuration for resolver config failed: %w", err)
	}

	if len(filtered) == 0 {
		return nil, nil
	}

	result := make([]*resolverspec.Resolver, 0, len(filtered))
	for _, r := range filtered {
		result = append(result, r.Resolvers...)
	}

	return result, nil
}

// ResolverProviderOptions configures the creation of a resolver provider.
type ResolverProviderOptions struct {
	// Registry is the component version repository provider
	Registry repository.ComponentVersionRepositoryProvider
	// CredentialGraph is used to resolve credentials for repositories
	CredentialGraph credentials.Resolver
	// Logger is used for logging resolver operations
	Logger *logr.Logger
	// Resolvers is the list of path matcher resolvers to use
	Resolvers []*resolverspec.Resolver
}

// NewResolverProviderWithRepository creates a resolver provider with a base repository
// and optional component patterns.
func NewResolverProviderWithRepository(
	ctx context.Context,
	opts ResolverProviderOptions,
	baseRepo runtime.Typed,
) (ComponentVersionRepositoryForComponentProvider, error) {
	if opts.Registry == nil {
		return nil, fmt.Errorf("component version registry is required")
	}

	if baseRepo == nil {
		return nil, fmt.Errorf("base repository is required")
	}

	raw := runtime.Raw{}
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	if err := scheme.Convert(baseRepo, &raw); err != nil {
		return nil, fmt.Errorf("converting repository spec to raw failed: %w", err)
	}

	// A combination of provided resolvers and the final catch-all parent repo spec.
	var finalResolvers []*resolverspec.Resolver

	// TODO: I don't think we need this since unlike in the cli we don't parse compref. Discuss.
	// Add component-specific patterns at the beginning (highest priority)
	// if len(componentPatterns) > 0 {
	//	for _, pattern := range componentPatterns {
	//		finalResolvers = append(finalResolvers, &resolverspec.Resolver{
	//			Repository:           &raw,
	//			ComponentNamePattern: pattern,
	//		})
	//	}
	// } else {
	// No specific patterns - add baseRepo wildcard first so explicit repositoryRef wins
	// finalResolvers = append(finalResolvers, &resolverspec.Resolver{
	//	 Repository:           &raw,
	//	 ComponentNamePattern: "*",
	// })
	// }

	// Configured resolvers should always take precedence as opposed to GIVEN repository spec that for the controller
	// is something that is always provided because the controllers always have a repository spec. Either from the
	// Repository object or from a config.
	finalResolvers = append(finalResolvers, opts.Resolvers...)

	// Finally, add our repositorySpec as last match all so all components will match the provider repository
	// spec in the end, closing the chain.
	finalResolvers = append(finalResolvers, &resolverspec.Resolver{
		Repository:           &raw,
		ComponentNamePattern: "*",
	})
	if opts.Logger != nil {
		opts.Logger.V(1).Info("creating resolver provider with repository and patterns",
			"baseRepository", baseRepo,
			"configResolverCount", len(opts.Resolvers),
			"finalResolverCount", len(finalResolvers))
	}

	return &resolverProvider{
		repoProvider: opts.Registry,
		graph:        opts.CredentialGraph,
		provider:     pathmatcher.NewSpecProvider(ctx, finalResolvers),
		logger:       opts.Logger,
	}, nil
}
