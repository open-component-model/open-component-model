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

type BuiltinComponentListerPluginContract interface {
	InternalComponentListerPluginContract
	GetComponentVersionRepositoryScheme() *runtime.Scheme
}
