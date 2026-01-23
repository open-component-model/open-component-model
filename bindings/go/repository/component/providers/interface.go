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
	GetComponentVersionRepositoryForComponent(ctx context.Context, component, version string) (repository.ComponentVersionRepository, error)
}

// SpecResolvingProvider extends ComponentVersionRepositoryForComponentProvider with the ability
// to return the resolved repository specification. This is useful for cache key generation
// when the actual spec depends on resolver pattern matching.
type SpecResolvingProvider interface {
	ComponentVersionRepositoryForComponentProvider
	GetRepositorySpecForComponent(ctx context.Context, component, version string) (runtime.Typed, error)
}
