package componentlister

import (
	"context"

	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type InternalComponentListerPluginContract interface {
	// GetComponentLister returns a component lister for the given repository specification.
	GetComponentLister(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (repository.ComponentLister, error)

	// GetComponentListerCredentialConsumerIdentity retrieves an identity for the given specification that
	// can be used to lookup credentials for the repository.
	GetComponentListerCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error)
}

// The BuiltinComponentListerPluginContract has the primary purpose to allow plugin
// registries to register internal plugins without requiring callers to
// explicitly provide a scheme with their supported types.
// A scheme is mapping types to their go types. As the go types of external
// plugins are not compiled in, they cannot have a scheme and therefore, cannot
// implement this interface.
type BuiltinComponentListerPluginContract interface {
	InternalComponentListerPluginContract
	GetComponentVersionRepositoryScheme() *runtime.Scheme
}
