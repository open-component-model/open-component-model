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

// NewComponentRepositoryProvider creates a ComponentVersionRepositoryForComponentProvider
// that resolves and returns the appropriate repository for a given component name.
//
// The provider evaluates component names against configured patterns or fallback resolvers to determine
// which repository specification should be used. This enables routing different components to different
// repositories based on their names (e.g., "github.com/*" components to one registry, "ocm.software/*" to another).
//
// The function supports two resolver types (mutually exclusive):
//  1. Path matcher resolvers (recommended) - pattern-based component name matching with priority ordering
//  2. Fallback resolvers (deprecated) - priority-based resolution without pattern matching
//
// Configuration options:
//   - WithConfig: Provide resolver configuration from config file
//   - WithRepository: Set the base repository specification
//   - WithComponentPatterns: Set component name patterns for high-priority resolvers
//   - WithComponentRef: Convenience option to set repository and component pattern from a component reference
//
// Returns an error if both resolver types are configured, or if no repository and no resolvers are provided.
func NewComponentRepositoryProvider(
	ctx context.Context,
	repoProvider repository.ComponentVersionRepositoryProvider,
	credentialGraph credentials.Resolver,
	opts ...RepositoryResolverOption,
) (ComponentVersionRepositoryForComponentProvider, error) {
	options := &RepositoryResolverOptions{}
	for _, opt := range opts {
		opt(options)
	}

	var (
		//nolint:staticcheck // compatibility mode for deprecated resolvers
		fallbackResolvers []*resolverruntime.Resolver
		pathMatchers      []*resolverspec.Resolver
		err               error
	)

	if options.config != nil {
		pathMatchers, err = ResolversFromConfig(options.config)
		if err != nil {
			return nil, fmt.Errorf("getting path matchers from configuration failed: %w", err)
		}
		fallbackResolvers, err = FallbackResolversFromConfig(options.config)
		if err != nil {
			return nil, fmt.Errorf("getting resolvers from configuration failed: %w", err)
		}
	}

	// Default to "*" if no component patterns specified
	if len(options.componentPatterns) == 0 {
		options.componentPatterns = []string{"*"}
	}

	switch {
	case len(pathMatchers) > 0 && len(fallbackResolvers) > 0:
		return nil, fmt.Errorf("both path matcher and fallback resolvers are configured, only one type is allowed")

	case len(fallbackResolvers) > 0:
		slog.WarnContext(ctx, "using deprecated fallback resolvers, consider switching to path matcher resolvers")
		return createFallbackProvider(ctx, repoProvider, credentialGraph, options.repository, fallbackResolvers)
	case len(pathMatchers) > 0:
		slog.DebugContext(ctx, "using path matcher resolvers", slog.Int("count", len(pathMatchers)))
		return createPathMatcherProvider(ctx, repoProvider, credentialGraph, options.repository, options.componentPatterns, pathMatchers)
	case len(pathMatchers) == 0 && len(fallbackResolvers) == 0 && options.repository != nil:
		slog.DebugContext(ctx, "no resolvers configured, using repository reference as resolver")
		return createSimplePathMatcherProvider(ctx, repoProvider, credentialGraph, options.repository)
	}
	return nil, nil
}

// createSimplePathMatcherProvider creates a resolver provider with a single wildcard matcher for the given repository.
func createSimplePathMatcherProvider(
	ctx context.Context,
	repoProvider repository.ComponentVersionRepositoryProvider,
	credentialGraph credentials.Resolver,
	repository runtime.Typed,
) (ComponentVersionRepositoryForComponentProvider, error) {
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
}

// createFallbackProvider creates a fallback resolver provider (deprecated).
//
//nolint:staticcheck // compatibility mode for deprecated resolvers
func createFallbackProvider(
	ctx context.Context,
	repoProvider repository.ComponentVersionRepositoryProvider,
	credentialGraph credentials.Resolver,
	repository runtime.Typed,
	fallbackResolvers []*resolverruntime.Resolver,
) (ComponentVersionRepositoryForComponentProvider, error) {
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

	return &fallbackProvider{
		repoProvider: repoProvider,
		graph:        credentialGraph,
		resolvers:    finalResolvers,
	}, nil
}

// createPathMatcherProvider creates a path matcher resolver provider with priority ordering.
func createPathMatcherProvider(
	ctx context.Context,
	repoProvider repository.ComponentVersionRepositoryProvider,
	credentialGraph credentials.Resolver,
	repository runtime.Typed,
	componentPatterns []string,
	pathMatchers []*resolverspec.Resolver,
) (ComponentVersionRepositoryForComponentProvider, error) {
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

// RepositoryResolverOptions holds configuration for NewComponentRepositoryProvider.
type RepositoryResolverOptions struct {
	config            *genericv1.Config
	repository        runtime.Typed
	componentPatterns []string
}

// RepositoryResolverOption is a function that configures RepositoryResolverOptions.
type RepositoryResolverOption func(*RepositoryResolverOptions)

// WithConfig sets the configuration containing resolver definitions.
// The config can contain both path matcher resolvers and fallback resolvers (deprecated).
func WithConfig(config *genericv1.Config) RepositoryResolverOption {
	return func(o *RepositoryResolverOptions) {
		o.config = config
	}
}

// WithRepository sets the repository specification.
func WithRepository(repo runtime.Typed) RepositoryResolverOption {
	return func(o *RepositoryResolverOptions) {
		o.repository = repo
	}
}

// WithComponentPatterns sets the component name patterns for high-priority resolvers.
func WithComponentPatterns(patterns []string) RepositoryResolverOption {
	return func(o *RepositoryResolverOptions) {
		o.componentPatterns = patterns
	}
}

// WithComponentRef sets the repository and component pattern from a component reference.
// This is a convenience function that extracts both repository and component name from a compref.Ref.
// If ref is nil or ref.Component is empty, only the repository is set (if present).
func WithComponentRef(ref *compref.Ref) RepositoryResolverOption {
	return func(o *RepositoryResolverOptions) {
		if ref == nil {
			return
		}
		o.repository = ref.Repository
		if ref.Component != "" {
			o.componentPatterns = []string{ref.Component}
		}
	}
}

// NewComponentVersionRepositoryForComponentProvider creates a new ComponentVersionRepositoryForComponentProvider based on the provided
// component reference and configuration.
//
// Behaviour depends on what is provided:
//   - Only ref (no config): Creates a simple resolver using ref.Repository with wildcard "*" pattern (ref.Component is ignored)
//   - Only config: Uses resolvers from config (path matchers or fallback resolvers - deprecated)
//   - Both ref and config: Config determines resolver type; ref.Component gets highest priority, config resolvers get middle priority,
//     ref.Repository with "*" gets lowest priority as catch-all
//
// Returns an error if the config contains both path matcher and fallback resolver types simultaneously (mutually exclusive).
// Returns an error if neither a componentReference nor a configuration is provided
func NewComponentVersionRepositoryForComponentProvider(ctx context.Context,
	repoProvider repository.ComponentVersionRepositoryProvider,
	credentialGraph credentials.Resolver,
	config *genericv1.Config,
	ref *compref.Ref,
) (ComponentVersionRepositoryForComponentProvider, error) {
	return NewComponentRepositoryProvider(ctx, repoProvider, credentialGraph, WithConfig(config), WithComponentRef(ref))
}
