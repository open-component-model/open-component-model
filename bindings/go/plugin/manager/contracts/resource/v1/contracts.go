package v1

import (
	"context"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type IdentityProvider[T runtime.Typed] interface {
	contracts.PluginBase
	GetIdentity(ctx context.Context, typ GetIdentityRequest[T]) (runtime.Identity, error)
}

type ReadResourcePluginContract interface {
	contracts.PluginBase
	IdentityProvider[runtime.Typed]
	GetGlobalResource(ctx context.Context, request GetResourceRequest, credentials map[string]string) (GetResourceResponse, error)
}

type WriteResourcePluginContract interface {
	contracts.PluginBase
	IdentityProvider[runtime.Typed]
	AddGlobalResource(ctx context.Context, request PostResourceRequest, credentials map[string]string) (*descriptor.Resource, error)
}

// ReadWriteResourcePluginContract is the contract defining Add and Get global resources.
type ReadWriteResourcePluginContract interface {
	ReadResourcePluginContract
	WriteResourcePluginContract
}
