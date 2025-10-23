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

type Options struct {
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
	config *genericv1.Config,
	ref *compref.Ref,
	options ...Options,
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

	switch {
	case len(pathMatchers) > 0 && len(fallbackResolvers) > 0:
		return nil, fmt.Errorf("both path matcher and fallback resolvers are configured, only one type is allowed")
	case len(pathMatchers) == 0 && len(fallbackResolvers) == 0 && ref != nil:
		slog.InfoContext(ctx, "no resolvers configured, using component reference as resolver")

		if ref.Repository == nil {
			return nil, fmt.Errorf("component reference does not contain repository information")
		}

		raw := runtime.Raw{}
		scheme := runtime.NewScheme(runtime.WithAllowUnknown())
		if err := scheme.Convert(ref.Repository, &raw); err != nil {
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

		// add compref as first entry to fallback list if available to mimic legacy behavior
		if ref != nil && ref.Repository != nil {
			//nolint:staticcheck // compatibility mode for deprecated resolvers
			var finalResolvers []*resolverruntime.Resolver
			if ref.Repository != nil {
				//nolint:staticcheck // kept for backward compatibility, use resolvers instead
				finalResolvers = append(finalResolvers, &resolverruntime.Resolver{
					Repository: ref.Repository,
					Priority:   math.MaxInt,
				})
			}
			finalResolvers = append(finalResolvers, fallbackResolvers...)
			fallbackResolvers = finalResolvers
		} else if options != nil && options[0].RepoRef != nil {
			var finalResolvers []*resolverruntime.Resolver
			finalResolvers = append(finalResolvers, fallbackResolvers...)

			finalResolvers = append([]*resolverruntime.Resolver{
				{
					Repository: options[0].RepoRef,
					Priority:   math.MaxInt,
				},
			}, finalResolvers...)

			fallbackResolvers = finalResolvers
		}

		return &fallbackProvider{
			repoProvider: repoProvider,
			graph:        credentialGraph,
			resolvers:    fallbackResolvers,
		}, nil
	default:
		slog.DebugContext(ctx, "using path matcher resolvers")

		if ref != nil && ref.Repository != nil {
			var finalResolvers []*resolverspec.Resolver
			finalResolvers = append(finalResolvers, pathMatchers...)
			raw := runtime.Raw{}
			scheme := runtime.NewScheme(runtime.WithAllowUnknown())
			if err := scheme.Convert(ref.Repository, &raw); err != nil {
				return nil, fmt.Errorf("converting repository spec to raw failed: %w", err)
			}

			compRefResolver := &resolverspec.Resolver{
				Repository:           &raw,
				ComponentNamePattern: ref.Component,
			}
			// add to index 0 to have the highest priority
			finalResolvers = append([]*resolverspec.Resolver{compRefResolver}, finalResolvers...)

			pathMatchers = finalResolvers
		} else if options != nil && options[0].RepoRef != nil {
			var finalResolvers []*resolverspec.Resolver
			finalResolvers = append(finalResolvers, pathMatchers...)

			raw := runtime.Raw{}
			scheme := runtime.NewScheme(runtime.WithAllowUnknown())
			if err := scheme.Convert(options[0].RepoRef, &raw); err != nil {
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
}
