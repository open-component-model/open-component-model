package ocm

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	resolverruntime "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/runtime"
	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/repository"
	pathmatcher "ocm.software/open-component-model/bindings/go/repository/component/pathmatcher/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

type RepositorySources struct {
	Config  *genericv1.Config
	CompRef *compref.Ref
	RepoRef runtime.Typed
}

// NewComponentVersionRepositoryForComponentProvider creates a new ComponentVersionRepositoryForComponentProvider based on the provided
// component reference and configuration.
// If a compref.Ref is provided, it will be used to create a compRefProvider.
// If a genericv1.Config is provided, it will be used to create either a fallback resolver provider (deprecated)
// or a path matcher resolver provider.
// If both types are configured, an error will be returned.
// If neither a componentReference nor a configuration is provided, an error will be returned.
// As a fallback, this constructor adds the compref as a fallback entry as both
// resolverruntime.Resolver (lowest priority) and resolverspec.Resolver (highest priority) depending on the configuration type.
// CAREFUL: may return nil
func NewComponentVersionRepositoryForComponentProvider(ctx context.Context,
	repoProvider repository.ComponentVersionRepositoryProvider,
	credentialGraph credentials.GraphResolver,
	repoSources RepositorySources,
) (ComponentVersionRepositoryForComponentProvider, error) {
	var (
		//nolint:staticcheck // compatibility mode for deprecated resolvers
		fallbackResolvers []*resolverruntime.Resolver
		pathMatchers      []*resolverspec.Resolver
		err               error
	)

	if repoSources.Config != nil {
		pathMatchers, err = ResolversFromConfig(repoSources.Config)
		if err != nil {
			return nil, fmt.Errorf("getting path matchers from configuration failed: %w", err)
		}
		fallbackResolvers, err = FallbackResolversFromConfig(repoSources.Config)
		if err != nil {
			return nil, fmt.Errorf("getting resolvers from configuration failed: %w", err)
		}
	}

	switch {
	case len(pathMatchers) > 0 && len(fallbackResolvers) > 0:
		return nil, fmt.Errorf("both path matcher and fallback resolvers are configured, only one type is allowed")
	case len(pathMatchers) == 0 && len(fallbackResolvers) == 0 && repoSources.CompRef != nil:
		slog.InfoContext(ctx, "no resolvers configured, using component reference as resolver")

		return createCompRefResolvers(ctx, repoProvider, credentialGraph, repoSources)
	case len(fallbackResolvers) > 0:
		slog.WarnContext(ctx, "using deprecated fallback resolvers, consider switching to path matcher resolvers")

		return createFallbackResolvers(fallbackResolvers, repoProvider, credentialGraph, repoSources)
	default:
		slog.DebugContext(ctx, "using path matcher resolvers")

		return createPathMatchersResolvers(ctx, repoProvider, credentialGraph, pathMatchers, repoSources)
	}
}

func createPathMatchersResolvers(ctx context.Context, repoProvider repository.ComponentVersionRepositoryProvider, credentialGraph credentials.GraphResolver, pathMatchers []*resolverspec.Resolver, repoSources RepositorySources) (ComponentVersionRepositoryForComponentProvider, error) {
	if repoSources.CompRef != nil && repoSources.CompRef.Repository != nil {
		var finalResolvers []*resolverspec.Resolver
		finalResolvers = append(finalResolvers, pathMatchers...)
		raw := runtime.Raw{}
		scheme := runtime.NewScheme(runtime.WithAllowUnknown())
		if err := scheme.Convert(repoSources.CompRef.Repository, &raw); err != nil {
			return nil, fmt.Errorf("converting repository spec to raw failed: %w", err)
		}

		compRefResolver := &resolverspec.Resolver{
			Repository:           &raw,
			ComponentNamePattern: repoSources.CompRef.Component,
		}
		// add to index 0 to have the highest priority
		finalResolvers = append([]*resolverspec.Resolver{compRefResolver}, finalResolvers...)

		pathMatchers = finalResolvers
	} else if repoSources.RepoRef != nil {
		var finalResolvers []*resolverspec.Resolver
		finalResolvers = append(finalResolvers, pathMatchers...)

		raw := runtime.Raw{}
		scheme := runtime.NewScheme(runtime.WithAllowUnknown())
		if err := scheme.Convert(repoSources.RepoRef, &raw); err != nil {
			return nil, fmt.Errorf("converting repository spec to raw failed: %w", err)
		}
		finalResolvers = append(finalResolvers, &resolverspec.Resolver{
			Repository:           &raw,
			ComponentNamePattern: "*",
		})

		pathMatchers = finalResolvers
	}

	if len(pathMatchers) == 0 {
		return nil, fmt.Errorf("no path matcher resolvers configured")
	}

	return &resolverProvider{
		repoProvider: repoProvider,
		graph:        credentialGraph,
		provider:     pathmatcher.NewSpecProvider(ctx, pathMatchers),
	}, nil
}

//nolint:staticcheck // compatibility mode for deprecated resolvers
func createFallbackResolvers(fallbackResolvers []*resolverruntime.Resolver, repoProvider repository.ComponentVersionRepositoryProvider, credentialGraph credentials.GraphResolver, repoSources RepositorySources) (ComponentVersionRepositoryForComponentProvider, error) {
	// add compref as first entry to fallback list if available to mimic legacy behavior
	if repoSources.CompRef != nil && repoSources.CompRef.Repository != nil {
		//nolint:staticcheck // compatibility mode for deprecated resolvers
		var finalResolvers []*resolverruntime.Resolver
		if repoSources.CompRef.Repository != nil {
			//nolint:staticcheck // kept for backward compatibility, use resolvers instead
			finalResolvers = append(finalResolvers, &resolverruntime.Resolver{
				Repository: repoSources.CompRef.Repository,
				Priority:   math.MaxInt,
			})
		}
		finalResolvers = append(finalResolvers, fallbackResolvers...)
		fallbackResolvers = finalResolvers
	} else if repoSources.RepoRef != nil {
		//nolint:staticcheck // compatibility mode for deprecated resolvers
		var finalResolvers []*resolverruntime.Resolver
		finalResolvers = append(finalResolvers, fallbackResolvers...)
		//nolint:staticcheck // compatibility mode for deprecated resolvers
		finalResolvers = append(finalResolvers, &resolverruntime.Resolver{
			Repository: repoSources.RepoRef,
			Priority:   math.MaxInt,
		})

		fallbackResolvers = finalResolvers
	}

	return &fallbackProvider{
		repoProvider: repoProvider,
		graph:        credentialGraph,
		resolvers:    fallbackResolvers,
	}, nil
}

func createCompRefResolvers(ctx context.Context, repoProvider repository.ComponentVersionRepositoryProvider, credentialGraph credentials.GraphResolver, repoSources RepositorySources) (ComponentVersionRepositoryForComponentProvider, error) {
	if repoSources.CompRef.Repository == nil {
		return nil, fmt.Errorf("component reference does not contain repository information")
	}

	raw := runtime.Raw{}
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	if err := scheme.Convert(repoSources.CompRef.Repository, &raw); err != nil {
		return nil, fmt.Errorf("converting repository spec to raw failed: %w", err)
	}

	return &resolverProvider{
		repoProvider: repoProvider,
		graph:        credentialGraph,
		provider: pathmatcher.NewSpecProvider(ctx, []*resolverspec.Resolver{
			{
				Repository:           &raw,
				ComponentNamePattern: "*",
			},
		}),
	}, nil
}
