package providers

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	resolverruntime "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/runtime"
	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/repository"
	//nolint:staticcheck // compatibility mode for deprecated resolvers
	v1 "ocm.software/open-component-model/bindings/go/repository/component/fallback/v1"
	pathmatcher "ocm.software/open-component-model/bindings/go/repository/component/pathmatcher/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Options configures the creation of a provider.
type Options struct {
	RepoProvider    repository.ComponentVersionRepositoryProvider
	CredentialGraph credentials.Resolver
	PathMatchers    []*resolverspec.Resolver
	//nolint:staticcheck // compatibility mode for deprecated resolvers
	FallbackResolvers []*resolverruntime.Resolver
	// ComponentPatterns specifies high-priority patterns for the base repository.
	// These patterns are prepended to the resolver list, giving them highest priority.
	// Used by CLI to route specific component references to the provided repository.
	ComponentPatterns []string
}

// New creates a ComponentVersionRepositoryForComponentProvider based on the provided options.
// It supports two resolver types (mutually exclusive):
//  1. Path matcher resolvers (v1alpha1) - pattern-based component name matching
//  2. Fallback resolvers (v1, deprecated) - priority-based resolution
//
// If baseRepo is provided, it is used as the catch-all for path matchers
// or as the highest priority entry for fallback resolvers.
//
// Returns an error if both resolver types are configured.
func New(
	ctx context.Context,
	opts Options,
	baseRepo runtime.Typed,
) (ComponentVersionRepositoryResolver, error) {
	if opts.RepoProvider == nil {
		return nil, fmt.Errorf("repository provider is required")
	}

	if len(opts.PathMatchers) > 0 && len(opts.FallbackResolvers) > 0 {
		return nil, fmt.Errorf("both path matcher and fallback resolvers are configured, only one type is allowed")
	}

	if len(opts.FallbackResolvers) > 0 {
		slog.WarnContext(ctx, "using deprecated fallback resolvers, consider switching to path matcher resolvers")
		return newFallbackProviderWithBaseRepo(ctx, opts, baseRepo)
	}

	return newPathMatcherProviderWithBaseRepo(ctx, opts, baseRepo)
}

//nolint:staticcheck // compatibility mode for deprecated resolvers
func newFallbackProviderWithBaseRepo(ctx context.Context, opts Options, baseRepo runtime.Typed) (ComponentVersionRepositoryResolver, error) {
	var finalResolvers []*resolverruntime.Resolver

	if baseRepo != nil {
		finalResolvers = append(finalResolvers, &resolverruntime.Resolver{
			Repository: baseRepo,
			Priority:   math.MaxInt,
		})
	}
	finalResolvers = append(finalResolvers, opts.FallbackResolvers...)

	fallbackRepo, err := v1.NewFallbackRepository(ctx, opts.RepoProvider, opts.CredentialGraph, finalResolvers)
	if err != nil {
		return nil, fmt.Errorf("creating fallback repository failed: %w", err)
	}

	return &fallbackResolver{
		repo: fallbackRepo,
	}, nil
}

func newPathMatcherProviderWithBaseRepo(ctx context.Context, opts Options, baseRepo runtime.Typed) (ComponentVersionRepositoryResolver, error) {
	var finalResolvers []*resolverspec.Resolver

	if baseRepo != nil {
		raw := runtime.Raw{}
		scheme := runtime.NewScheme(runtime.WithAllowUnknown())
		if err := scheme.Convert(baseRepo, &raw); err != nil {
			return nil, fmt.Errorf("converting repository spec to raw failed: %w", err)
		}

		// Component patterns get highest priority - prepend them
		for _, pattern := range opts.ComponentPatterns {
			finalResolvers = append(finalResolvers, &resolverspec.Resolver{
				Repository:           &raw,
				ComponentNamePattern: pattern,
			})
		}

		// Config resolvers come next
		finalResolvers = append(finalResolvers, opts.PathMatchers...)

		// Base repo as catch-all at the end
		finalResolvers = append(finalResolvers, &resolverspec.Resolver{
			Repository:           &raw,
			ComponentNamePattern: "*",
		})
	} else {
		finalResolvers = append(finalResolvers, opts.PathMatchers...)
	}

	if len(finalResolvers) == 0 {
		return nil, nil
	}

	provider := &pathMatcherResolver{
		repoProvider: opts.RepoProvider,
		graph:        opts.CredentialGraph,
		specProvider: pathmatcher.NewSpecProvider(ctx, finalResolvers),
		repoCache:    make(map[string]repository.ComponentVersionRepository),
	}

	return provider, nil
}
