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

// newComponentVersionRepositoryForComponentProviderInternal is the unified implementation for creating a
// ComponentVersionRepositoryForComponentProvider. It handles both single component references and multiple
// component patterns.
//
// Parameters:
//   - repository: The repository specification (can be nil if using only config-based resolvers)
//   - componentPatterns: Component name patterns to create high-priority resolvers for.
//     Use []string{componentName} for a single component, []string{"*"} for all, or multiple patterns.
//     If empty and repository is provided, defaults to "*" pattern.
//   - config: Configuration containing resolver definitions (can be nil)
//
// The function supports two resolver types (mutually exclusive):
//  1. Path matcher resolvers (recommended) - pattern-based component resolution
//  2. Fallback resolvers (deprecated) - priority-based resolution
//
// Returns an error if both resolver types are configured, or if no repository and no resolvers are provided.
func newComponentVersionRepositoryForComponentProviderInternal(ctx context.Context,
	repoProvider repository.ComponentVersionRepositoryProvider,
	credentialGraph credentials.GraphResolver,
	config *genericv1.Config,
	repository runtime.Typed,
	componentPatterns []string,
) (ComponentVersionRepositoryForComponentProvider, error) {
	var (
		//nolint:staticcheck // compatibility mode for deprecated resolvers
		fallbackResolvers []*resolverruntime.Resolver
		pathMatchers      []*resolverspec.Resolver
		err               error
	)

	if config != nil {
		pathMatchers, err = ResolversFromConfig(config)
		if err != nil {
			return nil, fmt.Errorf("getting path matchers from configuration failed: %w", err)
		}
		fallbackResolvers, err = FallbackResolversFromConfig(config)
		if err != nil {
			return nil, fmt.Errorf("getting resolvers from configuration failed: %w", err)
		}
	}

	// Default to "*" pattern if no patterns specified and repository is provided
	if len(componentPatterns) == 0 && repository != nil {
		componentPatterns = []string{"*"}
	}

	switch {
	case len(pathMatchers) > 0 && len(fallbackResolvers) > 0:
		return nil, fmt.Errorf("both path matcher and fallback resolvers are configured, only one type is allowed")
	case len(pathMatchers) == 0 && len(fallbackResolvers) == 0 && repository != nil:
		slog.InfoContext(ctx, "no resolvers configured, using repository reference as resolver")

		raw := runtime.Raw{}
		scheme := runtime.NewScheme(runtime.WithAllowUnknown())
		if err := scheme.Convert(repository, &raw); err != nil {
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
	case len(fallbackResolvers) > 0:
		slog.WarnContext(ctx, "using deprecated fallback resolvers, consider switching to path matcher resolvers")

		// add repository as first entry to fallback list if available to mimic legacy behavior
		//nolint:staticcheck // compatibility mode for deprecated resolvers
		var finalResolvers []*resolverruntime.Resolver
		if repository != nil {
			//nolint:staticcheck // kept for backward compatibility, use resolvers instead
			finalResolvers = append(finalResolvers, &resolverruntime.Resolver{
				Repository: repository,
				Priority:   math.MaxInt,
			})
		}
		finalResolvers = append(finalResolvers, fallbackResolvers...)
		fallbackResolvers = finalResolvers

		return &fallbackProvider{
			repoProvider: repoProvider,
			graph:        credentialGraph,
			resolvers:    fallbackResolvers,
		}, nil
	case len(pathMatchers) > 0:
		slog.DebugContext(ctx, "using path matcher resolvers", slog.Int("count", len(pathMatchers)))

		if repository != nil {
			var finalResolvers []*resolverspec.Resolver
			finalResolvers = append(finalResolvers, pathMatchers...)

			raw := runtime.Raw{}
			scheme := runtime.NewScheme(runtime.WithAllowUnknown())
			if err := scheme.Convert(repository, &raw); err != nil {
				return nil, fmt.Errorf("converting repository spec to raw failed: %w", err)
			}

			// Create high-priority resolvers for each component pattern
			componentMatchers := make([]*resolverspec.Resolver, 0, len(componentPatterns))
			for _, pattern := range componentPatterns {
				componentMatchers = append(componentMatchers, &resolverspec.Resolver{
					Repository:           &raw,
					ComponentNamePattern: pattern,
				})
			}

			// add component matchers to index 0 to have the highest priority
			finalResolvers = append(componentMatchers, finalResolvers...)

			// Add wildcard matcher at the end as catch-all
			finalResolvers = append(finalResolvers, &resolverspec.Resolver{
				Repository:           &raw,
				ComponentNamePattern: "*",
			})

			pathMatchers = finalResolvers
		}

		return &resolverProvider{
			repoProvider: repoProvider,
			graph:        credentialGraph,
			provider:     pathmatcher.NewSpecProvider(ctx, pathMatchers),
		}, nil
	}
	return nil, nil
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
func NewComponentVersionRepositoryForComponentProvider(ctx context.Context,
	repoProvider repository.ComponentVersionRepositoryProvider,
	credentialGraph credentials.GraphResolver,
	config *genericv1.Config,
	ref *compref.Ref,
) (ComponentVersionRepositoryForComponentProvider, error) {
	var repository runtime.Typed
	var componentPatterns []string

	if ref != nil {
		repository = ref.Repository
		if ref.Component != "" {
			componentPatterns = []string{ref.Component}
		}
	}

	return newComponentVersionRepositoryForComponentProviderInternal(
		ctx,
		repoProvider,
		credentialGraph,
		config,
		repository,
		componentPatterns,
	)
}

// NewComponentVersionRepositoryForComponentProviderWithPatterns creates a ComponentVersionRepositoryForComponentProvider
// for a repository with specific component name patterns. This is useful when you have a repository reference
// and want to create high-priority resolvers for specific components.
//
// This is a convenience wrapper around newComponentVersionRepositoryForComponentProviderInternal for cases where
// you have a repository and component patterns directly, rather than a compref.Ref.
func NewComponentVersionRepositoryForComponentProviderWithPatterns(ctx context.Context,
	repoProvider repository.ComponentVersionRepositoryProvider,
	credentialGraph credentials.GraphResolver,
	config *genericv1.Config,
	repository runtime.Typed,
	componentPatterns []string,
) (ComponentVersionRepositoryForComponentProvider, error) {
	return newComponentVersionRepositoryForComponentProviderInternal(
		ctx,
		repoProvider,
		credentialGraph,
		config,
		repository,
		componentPatterns,
	)
}
