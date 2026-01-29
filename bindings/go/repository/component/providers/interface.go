package providers

import (
	"context"

	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ComponentVersionRepositoryForComponentProvider provides a [repository.ComponentVersionRepository]
// based on a given component identity. Implementations may use different strategies to resolve
// the repository, such as pattern matching or priority-based fallback resolvers.
type ComponentVersionRepositoryForComponentProvider interface {
	// GetComponentVersionRepositoryForComponent returns a [repository.ComponentVersionRepository]
	// based on a given component identity.
	// This is the main method of this interface.
	GetComponentVersionRepositoryForComponent(ctx context.Context, component, version string) (repository.ComponentVersionRepository, error)

	// GetComponentVersionRepositoryForSpecification returns a [repository.ComponentVersionRepository]
	// based on a given repository specification. The repository specification is expected to be part
	// of any of the providers resolver configurations.
	// This is a convenience method to access a providers underlying repositories.
	GetComponentVersionRepositoryForSpecification(ctx context.Context, specification runtime.Typed) (repository.ComponentVersionRepository, error)
}
