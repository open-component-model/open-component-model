package setup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

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
	GetComponentVersionRepositoryForComponent(ctx context.Context, component, version string) (repository.ComponentVersionRepository, error)
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

// NewResolverProvider creates a new resolver provider using path matcher resolvers.
// The provider uses the configured resolvers to match component names against patterns
// and determine which repository specification to use for resolving component versions.
func NewResolverProvider(ctx context.Context, opts ResolverProviderOptions) (ComponentVersionRepositoryForComponentProvider, error) {
	if opts.Registry == nil {
		return nil, fmt.Errorf("component version registry is required")
	}

	if len(opts.Resolvers) == 0 {
		return nil, fmt.Errorf("at least one resolver must be provided")
	}

	// Log resolver configuration
	if opts.Logger != nil {
		opts.Logger.V(1).Info("creating resolver provider with path matcher resolvers",
			"resolverCount", len(opts.Resolvers))
		for i, resolver := range opts.Resolvers {
			opts.Logger.V(2).Info("resolver configuration",
				"index", i,
				"pattern", resolver.ComponentNamePattern,
				"repositoryType", resolver.Repository.Name)
		}
	}

	return &resolverProvider{
		repoProvider: opts.Registry,
		graph:        opts.CredentialGraph,
		provider:     pathmatcher.NewSpecProvider(ctx, opts.Resolvers),
		logger:       opts.Logger,
	}, nil
}

// NewResolverProviderWithRepository creates a resolver provider with a base repository
// and optional component patterns. This is a convenience function that creates resolvers
// with the following priority ordering:
//  1. Component-specific patterns (if provided) - highest priority
//  2. Config-based resolvers (if provided) - middle priority
//  3. Wildcard catch-all using baseRepo - lowest priority
//
// This allows component-specific overrides to take precedence over general configuration.
func NewResolverProviderWithRepository(
	ctx context.Context,
	opts ResolverProviderOptions,
	baseRepo runtime.Typed,
	componentPatterns []string,
) (ComponentVersionRepositoryForComponentProvider, error) {
	if opts.Registry == nil {
		return nil, fmt.Errorf("component version registry is required")
	}

	if baseRepo == nil {
		return nil, fmt.Errorf("base repository is required")
	}

	// Convert baseRepo to raw
	raw := runtime.Raw{}
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	if err := scheme.Convert(baseRepo, &raw); err != nil {
		return nil, fmt.Errorf("converting repository spec to raw failed: %w", err)
	}

	// Start with config-based resolvers (if any)
	var finalResolvers []*resolverspec.Resolver
	if len(opts.Resolvers) > 0 {
		finalResolvers = append(finalResolvers, opts.Resolvers...)
	}

	// Add component-specific patterns at the beginning (highest priority)
	if len(componentPatterns) > 0 {
		componentMatchers := make([]*resolverspec.Resolver, 0, len(componentPatterns))
		for _, pattern := range componentPatterns {
			componentMatchers = append(componentMatchers, &resolverspec.Resolver{
				Repository:           &raw,
				ComponentNamePattern: pattern,
			})
		}
		finalResolvers = append(componentMatchers, finalResolvers...)
	} else {
		// If no component patterns specified, use wildcard at the beginning
		finalResolvers = append([]*resolverspec.Resolver{
			{
				Repository:           &raw,
				ComponentNamePattern: "*",
			},
		}, finalResolvers...)
	}

	// Add wildcard matcher at the end as catch-all
	finalResolvers = append(finalResolvers, &resolverspec.Resolver{
		Repository:           &raw,
		ComponentNamePattern: "*",
	})

	if opts.Logger != nil {
		opts.Logger.V(1).Info("creating resolver provider with repository and patterns",
			"baseRepository", baseRepo,
			"componentPatterns", componentPatterns,
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

// NewSimpleResolverProvider creates a resolver provider with a single wildcard matcher
// for the given repository. This is the simplest form of resolver that matches all components.
func NewSimpleResolverProvider(
	ctx context.Context,
	opts ResolverProviderOptions,
	repository runtime.Typed,
) (ComponentVersionRepositoryForComponentProvider, error) {
	if opts.Registry == nil {
		return nil, fmt.Errorf("component version registry is required")
	}

	if repository == nil {
		return nil, fmt.Errorf("repository is required")
	}

	raw := runtime.Raw{}
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	if err := scheme.Convert(repository, &raw); err != nil {
		return nil, fmt.Errorf("converting repository spec to raw failed: %w", err)
	}

	resolvers := []*resolverspec.Resolver{
		{
			Repository:           &raw,
			ComponentNamePattern: "*",
		},
	}

	if opts.Logger != nil {
		opts.Logger.V(1).Info("creating simple resolver provider with wildcard matcher",
			"repository", repository)
	}

	return &resolverProvider{
		repoProvider: opts.Registry,
		graph:        opts.CredentialGraph,
		provider:     pathmatcher.NewSpecProvider(ctx, resolvers),
		logger:       opts.Logger,
	}, nil
}

// logResolverWarning logs a warning about deprecated fallback resolvers using slog if available.
func logResolverWarning(ctx context.Context) {
	slog.WarnContext(ctx, "using deprecated fallback resolvers, consider switching to path matcher resolvers (v1alpha1)")
}
