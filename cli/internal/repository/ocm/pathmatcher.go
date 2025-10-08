package ocm

import (
	"context"
	"fmt"
	"log/slog"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository"
	pathmatcher "ocm.software/open-component-model/bindings/go/repository/component/pathmatcher/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

func convertToRaw(repository runtime.Typed) (*runtime.Raw, error) {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	raw := runtime.Raw{}
	err := scheme.Convert(repository, &raw)
	if err != nil {
		return nil, fmt.Errorf("conversion to raw failed: %w", err)
	}
	return &raw, nil
}

// NewFromRefWithPathMatcher creates a new ComponentRepository instance for the given component reference.
// It resolves the appropriate plugin and credentials for the repository.
func NewFromRefWithPathMatcher(ctx context.Context, manager *manager.PluginManager, graph credentials.GraphResolver,
	resolvers []*resolverspec.Resolver, componentReference string) (*compref.Ref, ComponentVersionRepositoryProvider, error) {
	ref, err := compref.Parse(componentReference)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing component reference %q failed: %w", componentReference, err)
	}
	if len(resolvers) == 0 {
		resolvers = make([]*resolverspec.Resolver, 0)
	}

	if ref.Repository != nil {
		raw, err := convertToRaw(ref.Repository)
		if err != nil {
			return nil, nil, fmt.Errorf("converting repository to raw failed: %w", err)
		}

		resolvers = append(resolvers, &resolverspec.Resolver{
			Repository:           raw,
			ComponentNamePattern: ref.Component,
		})
	}

	provider := pathmatcher.NewSpecProvider(ctx, resolvers)

	return ref, func(ctx context.Context, identity *runtime.Identity) (repository.ComponentVersionRepository, error) {
		if identity == nil {
			identity = &runtime.Identity{
				descruntime.IdentityAttributeName: ref.Component,
			}
		}
		repoSpec, err := provider.GetRepositorySpec(ctx, *identity)
		if err != nil {
			return nil, fmt.Errorf("getting repository spec for component reference %q failed: %w", componentReference, err)
		}

		var credMap map[string]string
		consumerIdentity, err := manager.ComponentVersionRepositoryRegistry.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, ref.Repository)
		if err == nil {
			if graph != nil {
				if credMap, err = graph.Resolve(ctx, consumerIdentity); err != nil {
					slog.DebugContext(ctx, fmt.Sprintf("resolving credentials for repository %q failed: %s", ref.Repository, err.Error()))
				}
			}
		} else {
			slog.WarnContext(ctx, "could not get credential consumer identity for component version repository", "repository", ref.Repository, "error", err)
		}

		base, err := manager.ComponentVersionRepositoryRegistry.GetComponentVersionRepository(ctx, repoSpec, credMap)
		if err != nil {
			return nil, fmt.Errorf("getting component version repository for %q failed: %w", ref.Repository, err)
		}

		return base, nil
	}, nil
}

func ResolversFromConfig(config *genericv1.Config) ([]*resolverspec.Resolver, error) {
	filtered, err := genericv1.FilterForType[*resolverspec.Config](resolverspec.Scheme, config)
	if err != nil {
		return nil, fmt.Errorf("filtering configuration for resolver config failed: %w", err)
	}

	result := make([]*resolverspec.Resolver, 0, len(filtered))
	for _, r := range filtered {
		result = append(result, r.Resolvers...)
	}

	return result, nil
}
