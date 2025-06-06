package v1

import (
	"context"

	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type IdentityProvider[T runtime.Typed] interface {
	contracts.PluginBase
	GetIdentity(ctx context.Context, typ *GetIdentityRequest[T]) (*GetIdentityResponse, error)
}

type ResourceDigestProcessorContract interface {
	contracts.PluginBase
	IdentityProvider[runtime.Typed]
	ProcessResourceDigest(ctx context.Context, resource *ProcessResourceDigestRequest, credentials map[string]string) (*ProcessResourceDigestResponse, error)
}
