package helm

import (
	"context"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ResourceConsumerIdentityProvider is an interface that can be implemented to provide the identity of a resource consumer for credential resolution.
//
// Deprecated: Use repository.ResourceRepository instead - ResourceConsumerIdentityProvider will be removed once cli and transfer use the new repository.ResourceRepository
type ResourceConsumerIdentityProvider interface {
	// GetResourceCredentialConsumerIdentity resolves the identity of the given [descruntime.Resource] to use for credential resolution.
	GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descruntime.Resource) (identity runtime.Identity, err error)
}
