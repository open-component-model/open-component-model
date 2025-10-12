package componentlister

import (
	"context"

	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type InternalComponentListerPluginContract interface {
	repository.ComponentLister

	// GetComponentListerCredentialConsumerIdentity retrieves an identity for the given specification that
	// can be used to lookup credentials for the repository.
	GetComponentListerCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error)
}
