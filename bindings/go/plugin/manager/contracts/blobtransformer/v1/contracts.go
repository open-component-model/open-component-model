package v1

import (
	"context"

	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// IdentityProvider provides a way to retrieve the identity of a plugin. This identity can then further be used to resolve
// credentials for a specific plugin.
type IdentityProvider[T runtime.Typed] interface {
	contracts.PluginBase
	GetIdentity(ctx context.Context, typ *GetIdentityRequest[T]) (*GetIdentityResponse, error)
}

type BlobTransformerPluginContract[T runtime.Typed] interface {
	contracts.PluginBase
	IdentityProvider[T]
	TransformBlob(ctx context.Context, request *TransformBlobRequest[T], credentials map[string]string) (*TransformBlobResponse, error)
}
