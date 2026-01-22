package ocm

import (
	"context"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/repository/component/provider"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

// NewComponentRepositoryProvider creates a provider that resolves and returns
// the appropriate repository for a given component name.
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
) (provider.ComponentVersionRepositoryForComponentProvider, error) {
	options := &RepositoryResolverOptions{}
	for _, opt := range opts {
		opt(options)
	}

	fallbackResolvers, pathMatchers, err := provider.ExtractResolvers(options.config)
	if err != nil {
		return nil, err
	}

	// Default to "*" if no component patterns specified
	componentPatterns := options.componentPatterns
	if len(componentPatterns) == 0 {
		componentPatterns = []string{"*"}
	}

	providerOpts := provider.Options{
		RepoProvider:      repoProvider,
		CredentialGraph:   credentialGraph,
		PathMatchers:      pathMatchers,
		FallbackResolvers: fallbackResolvers,
		ComponentPatterns: componentPatterns,
	}

	return provider.New(ctx, providerOpts, options.repository)
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

// NewComponentVersionRepositoryForComponentProvider creates a provider based on the provided
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
) (provider.ComponentVersionRepositoryForComponentProvider, error) {
	return NewComponentRepositoryProvider(ctx, repoProvider, credentialGraph, WithConfig(config), WithComponentRef(ref))
}
