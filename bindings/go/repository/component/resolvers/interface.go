package providers

import (
	"context"

	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ComponentVersionRepositoryResolver provides a [repository.ComponentVersionRepository]
// based on a given component identity. Implementations may use different strategies to resolve
// the repository, such as pattern matching or priority-based fallback resolvers.
type ComponentVersionRepositoryResolver interface {
	// GetComponentVersionRepositoryForComponent returns a [repository.ComponentVersionRepository]
	// based on a given component identity.
	GetComponentVersionRepositoryForComponent(ctx context.Context, component, version string) (repository.ComponentVersionRepository, error)

	// GetComponentVersionRepositoryForSpecification returns a [repository.ComponentVersionRepository]
	// based on a given repository specification.
	GetComponentVersionRepositoryForSpecification(ctx context.Context, specification runtime.Typed) (repository.ComponentVersionRepository, error)
}
