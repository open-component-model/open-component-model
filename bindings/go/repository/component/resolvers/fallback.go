package providers

import (
	"context"

	"ocm.software/open-component-model/bindings/go/repository"
	//nolint:staticcheck // compatibility mode for deprecated resolvers
	fallback "ocm.software/open-component-model/bindings/go/repository/component/fallback/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// fallbackResolver provides a [repository.ComponentVersionRepository] based on deprecated fallback resolvers.
// This is kept for backward compatibility with the deprecated "ocm.config.ocm.software/v1" config type.
type fallbackResolver struct {
	//nolint:staticcheck // compatibility mode for deprecated resolvers
	repo *fallback.FallbackRepository
}

var _ ComponentVersionRepositoryResolver = (*fallbackResolver)(nil)

func (f *fallbackResolver) GetComponentVersionRepositoryForSpecification(ctx context.Context, specification runtime.Typed) (repository.ComponentVersionRepository, error) {
	return f.repo.GetComponentVersionRepositoryForSpecification(ctx, specification)
}

func (f *fallbackResolver) GetComponentVersionRepositoryForComponent(ctx context.Context, _, _ string) (repository.ComponentVersionRepository, error) {
	return f.repo, nil
}
