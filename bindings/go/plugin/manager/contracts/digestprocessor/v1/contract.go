package v1

import (
	"context"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type IdentityProvider[T runtime.Typed] interface {
	contracts.PluginBase
	GetIdentity(ctx context.Context, typ *GetIdentityRequest[T]) (*GetIdentityResponse, error)
}

type ResourceDigestProcessorPlugin interface {
	contracts.PluginBase
	IdentityProvider[runtime.Typed]
	ProcessResourceDigest(ctx context.Context, resource descriptor.Resource, credentials map[string]string) (*descriptor.Resource, error)
}
