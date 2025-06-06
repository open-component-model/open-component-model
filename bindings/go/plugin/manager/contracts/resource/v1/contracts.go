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

type ReadResourcePluginContract interface {
	contracts.PluginBase
	IdentityProvider[runtime.Typed]
	GetGlobalResource(ctx context.Context, request *GetResourceRequest, credentials map[string]string) (*GetResourceResponse, error)
}

type WriteResourcePluginContract interface {
	contracts.PluginBase
	IdentityProvider[runtime.Typed]
	AddGlobalResource(ctx context.Context, request *PostResourceRequest, credentials map[string]string) (*GetGlobalResourceResponse, error)
}

// ReadWriteResourcePluginContract is the contract defining Add and Get global resources.
type ReadWriteResourcePluginContract interface {
	ReadResourcePluginContract
	WriteResourcePluginContract
}
