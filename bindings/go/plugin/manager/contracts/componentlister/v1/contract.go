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

// ComponentListerPluginContract defines plug-in API for contents listing on OCM repositories.
type ComponentListerPluginContract[T runtime.Typed] interface {
	contracts.PluginBase
	IdentityProvider[T]

	// ListComponents is a single call to an implementing plugin.
	// If pagination is supported, a single page of component names is returned.
	ListComponents(ctx context.Context, request *ListComponentsRequest[T], credentials map[string]string) (*ListComponentsResponse, error)
}
