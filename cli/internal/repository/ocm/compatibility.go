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
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

type proxyProvider struct {
	compRefProv *compRefProvider
	provider    ComponentVersionRepositoryProvider
}

// NewComponentVersionRepositoryProvider TODO
func NewComponentVersionRepositoryProvider(ctx context.Context,
	pluginManager *manager.PluginManager,
	credentialGraph credentials.GraphResolver,
	config *genericv1.Config,
	componentReference string,
	options ...compref.Option,
) (ComponentVersionRepositoryProvider, error) {
	var (
		//nolint:staticcheck // compatibility mode for deprecated resolvers
		fallbackResolvers []*resolverruntime.Resolver
		pathMatchers      []*resolverspec.Resolver
		compRefProv       *compRefProvider
		provider          ComponentVersionRepositoryProvider
		err               error
	)

	var ref *compref.Ref
	if componentReference != "" {
		ref, err = compref.Parse(componentReference)
		if err != nil {
			return nil, fmt.Errorf("parsing component reference failed: %w", err)
		}
		compRefProv, err = newFromCompRef(componentReference, pluginManager, credentialGraph, options...)
		if err != nil {
			return nil, fmt.Errorf("parsing component reference failed: %w", err)
		}
	}

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

	if len(pathMatchers) > 0 && len(fallbackResolvers) > 0 {
		return nil, fmt.Errorf("both path matcher and fallback resolvers are configured, only one type is allowed")
	}

	// only use fallback resolvers if we got them from config and there are no path matchers
	if len(pathMatchers) == 0 {
		slog.WarnContext(ctx, "using deprecated fallback resolvers, consider switching to path matcher resolvers")

		// add compref as first entry to fallback list if available to mimic legacy behavior
		if ref != nil {
			//nolint:staticcheck // compatibility mode for deprecated resolvers
			var finalResolvers []*resolverruntime.Resolver
			if ref.Repository != nil {
				//nolint:staticcheck // no replacement for resolvers available yet https://github.com/open-component-model/ocm-project/issues/575
				finalResolvers = append(finalResolvers, &resolverruntime.Resolver{
					Repository: ref.Repository,
					Priority:   math.MaxInt,
				})
			}
			finalResolvers = append(finalResolvers, fallbackResolvers...)
			fallbackResolvers = finalResolvers
		}

		provider = newFromConfigWithFallback(pluginManager, credentialGraph, fallbackResolvers)
	} else {
		slog.DebugContext(ctx, "using path matcher resolvers", slog.Int("count", len(pathMatchers)))

		// add compref as last entry to path matcher list if available as fallback
		if ref != nil {
			var finalResolvers []*resolverspec.Resolver
			finalResolvers = append(finalResolvers, pathMatchers...)
			if ref.Repository != nil {
				raw := runtime.Raw{}
				scheme := runtime.NewScheme(runtime.WithAllowUnknown())
				if err := scheme.Convert(ref.Repository, &raw); err != nil {
					return nil, fmt.Errorf("converting repository spec to raw failed: %w", err)
				}

				finalResolvers = append(finalResolvers, &resolverspec.Resolver{
					Repository:           &raw,
					ComponentNamePattern: "**",
				})
			}

			pathMatchers = finalResolvers
		}

		provider, err = newFromConfigWithPathMatcher(ctx, pluginManager, credentialGraph, pathMatchers)
		if err != nil {
			return nil, fmt.Errorf("creating path matcher provider failed: %w", err)
		}
	}

	return &proxyProvider{
		compRefProv: compRefProv,
		provider:    provider,
	}, nil
}

func (p proxyProvider) GetComponentVersionRepository(ctx context.Context, identity runtime.Identity) (repository.ComponentVersionRepository, error) {
	if p.compRefProv != nil {
		// check if the identity matches the component reference repository
		if identity.Equal(p.compRefProv.ref.Identity()) {
			return p.compRefProv.GetComponentVersionRepository(ctx, identity)
		}
	}

	if p.provider != nil {
		return p.provider.GetComponentVersionRepository(ctx, identity)
	}

	return nil, fmt.Errorf("no component version repository found for identity %q", identity.String())
}
