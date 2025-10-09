package ocm

import (
	"context"
	"fmt"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	resolverruntime "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/runtime"
	resolverv1 "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"

	//nolint:staticcheck // compatibility mode for deprecated resolvers
	fallback "ocm.software/open-component-model/bindings/go/repository/component/fallback/v1"
)

type fallbackProvider struct {
	manager   *manager.PluginManager
	graph     credentials.GraphResolver
	resolvers []*resolverruntime.Resolver
}

func newFromConfigWithFallback(
	manager *manager.PluginManager,
	graph credentials.GraphResolver,
	resolvers []*resolverruntime.Resolver) *fallbackProvider {
	// TODO: add a fallback entry with wildcard * which is being injected by constructor as resolver
	// set as MAX PRIO in this case
	return &fallbackProvider{
		manager:   manager,
		graph:     graph,
		resolvers: resolvers,
	}
}

func (f *fallbackProvider) GetComponentVersionRepository(ctx context.Context, _ runtime.Identity) (repository.ComponentVersionRepository, error) {
	//nolint:staticcheck // no replacement for resolvers available yet https://github.com/open-component-model/ocm-project/issues/575
	fallbackRepo, err := fallback.NewFallbackRepository(ctx, f.manager.ComponentVersionRepositoryRegistry, f.graph, f.resolvers)
	if err != nil {
		return nil, fmt.Errorf("creating fallback repository failed: %w", err)
	}

	return fallbackRepo, nil
}

//nolint:staticcheck // no replacement for resolvers available yet https://github.com/open-component-model/ocm-project/issues/575
func FallbackResolversFromConfig(config *genericv1.Config) ([]*resolverruntime.Resolver, error) {
	filtered, err := genericv1.FilterForType[*resolverv1.Config](resolverv1.Scheme, config)
	if err != nil {
		return nil, fmt.Errorf("filtering configuration for resolver config failed: %w", err)
	}
	resolverConfigV1 := resolverv1.Merge(filtered...)

	resolverConfig, err := resolverruntime.ConvertFromV1(ocirepository.Scheme, resolverConfigV1)
	if err != nil {
		return nil, fmt.Errorf("converting resolver configuration from v1 to runtime failed: %w", err)
	}
	var resolvers []*resolverruntime.Resolver
	if resolverConfig != nil && len(resolverConfig.Resolvers) > 0 {
		resolvers = make([]*resolverruntime.Resolver, len(resolverConfig.Resolvers))
		for index, resolver := range resolverConfig.Resolvers {
			resolvers[index] = &resolver
		}
	}
	return resolvers, nil
}
