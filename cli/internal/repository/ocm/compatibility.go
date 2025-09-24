package ocm

import (
	"context"
	"fmt"
	"log/slog"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	resolverruntime "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/runtime"
	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
)

// NewFromRefWithResolvers creates a new ComponentRepository instance for the given component reference.
// It resolves the appropriate plugin and credentials for the repository.
// It prefers pathmatcher resolvers if available in the config, otherwise it falls back to
// using fallback resolvers.
func NewFromRefWithResolvers(ctx context.Context, pluginManager *manager.PluginManager, credentialGraph credentials.GraphResolver, config *genericv1.Config, componentReference string) (ComponentRepositoryProvider, error) {
	var (
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
	// prefer path matchers if available
	if len(pathMatchers) > 0 {
		slog.DebugContext(ctx, "using path matcher resolvers", slog.Int("count", len(pathMatchers)))
		return NewFromRefWithPathMatcher(ctx, pluginManager, credentialGraph, pathMatchers, componentReference)
	}

	slog.DebugContext(ctx, "using fallback resolvers", slog.Int("count", len(fallbackResolvers)))
	return NewFromRefWithFallbackRepo(ctx, pluginManager, credentialGraph, fallbackResolvers, componentReference)
}
