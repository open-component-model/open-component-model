package v1

import (
	"context"

	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type SignerPluginContract[T runtime.Typed] interface {
	contracts.PluginBase
	GetSignerIdentity(ctx context.Context, typ *GetSignerIdentityRequest[T]) (*IdentityResponse, error)
	Sign(ctx context.Context, request *SignRequest[T], credentials map[string]string) (*SignResponse, error)
}

type VerifierPluginContract[T runtime.Typed] interface {
	contracts.PluginBase
	GetVerifierIdentity(ctx context.Context, typ *GetVerifierIdentityRequest[T]) (*IdentityResponse, error)
	Verify(ctx context.Context, request *VerifyRequest[T], credentials map[string]string) (*VerifyResponse, error)
}

type SignatureHandlerContract[T runtime.Typed] interface {
	SignerPluginContract[T]
	VerifierPluginContract[T]
}
