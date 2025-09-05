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

type SignerPluginContract[T runtime.Typed] interface {
	contracts.PluginBase
	IdentityProvider[T]
	Sign(ctx context.Context, request *SignRequest[T], credentials map[string]string) (*SignResponse, error)
}

type VerifierPluginContract[T runtime.Typed] interface {
	contracts.PluginBase
	IdentityProvider[T]
	Verify(ctx context.Context, request *VerifyRequest[T], credentials map[string]string) (*VerifyResponse, error)
}

type SignatureHandlerContract[T runtime.Typed] interface {
	SignerPluginContract[T]
	VerifierPluginContract[T]
}
