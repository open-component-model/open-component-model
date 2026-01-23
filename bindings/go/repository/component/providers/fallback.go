package providers

import (
	"context"
	"fmt"

	resolverruntime "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/runtime"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/repository"
	//nolint:staticcheck // compatibility mode for deprecated resolvers
	fallback "ocm.software/open-component-model/bindings/go/repository/component/fallback/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// fallbackProvider provides a [repository.ComponentVersionRepository] based on deprecated fallback resolvers.
// This is kept for backward compatibility with the deprecated "ocm.config.ocm.software/v1" config type.
//
//nolint:staticcheck // compatibility mode for deprecated resolvers
type fallbackProvider struct {
	repoProvider repository.ComponentVersionRepositoryProvider
	graph        credentials.Resolver
	resolvers    []*resolverruntime.Resolver
	baseRepo     runtime.Typed
}

var _ SpecResolvingProvider = (*fallbackProvider)(nil)

func (f *fallbackProvider) GetRepositorySpecForComponent(_ context.Context, _, _ string) (runtime.Typed, error) {
	return f.baseRepo, nil
}

//nolint:staticcheck // compatibility mode for deprecated resolvers
func (f *fallbackProvider) GetComponentVersionRepositoryForComponent(ctx context.Context, _, _ string) (repository.ComponentVersionRepository, error) {
	repo, err := fallback.NewFallbackRepository(ctx, f.repoProvider, f.graph, f.resolvers)
	if err != nil {
		return nil, fmt.Errorf("creating fallback repository failed: %w", err)
	}
	return repo, nil
}
