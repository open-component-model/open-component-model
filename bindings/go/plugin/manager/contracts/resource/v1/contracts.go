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

type ReadResourcePluginContract interface {
	contracts.PluginBase
	IdentityProvider[runtime.Typed]
	GetGlobalResource(ctx context.Context, request *GetGlobalResourceRequest, credentials map[string]string) (*GetGlobalResourceResponse, error)
}

type WriteResourcePluginContract interface {
	contracts.PluginBase
	IdentityProvider[runtime.Typed]
	AddGlobalResource(ctx context.Context, request *AddGlobalResourceRequest, credentials map[string]string) (*AddGlobalResourceResponse, error)
}

// ReadWriteResourcePluginContract is the contract defining Add and Get global resources.
type ReadWriteResourcePluginContract interface {
	ReadResourcePluginContract
	WriteResourcePluginContract
}
