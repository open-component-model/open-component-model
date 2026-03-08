package helm

import (
	"context"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ResourceConsumerIdentityProvider is an interface that can be implemented to provide the identity of a resource consumer for credential resolution.
//
//	// TODO(matthiasbruns): Introduce a helm based ResourceRepository and handle identity in there https://github.com/open-component-model/ocm-project/issues/911
type ResourceConsumerIdentityProvider interface {
	// GetResourceCredentialConsumerIdentity resolves the identity of the given [descruntime.Resource] to use for credential resolution.
	GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descruntime.Resource) (identity runtime.Identity, err error)
}
