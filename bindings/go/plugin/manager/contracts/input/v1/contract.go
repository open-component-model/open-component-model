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

// ResourceInputPluginContract is a REST wrapper around the constructor.ProcessResource interface for communicating
// with a plugin.
type ResourceInputPluginContract interface {
	contracts.PluginBase
	IdentityProvider[runtime.Typed]
	ProcessResource(ctx context.Context, request *ProcessResourceInputRequest, credentials map[string]string) (*ProcessResourceInputResponse, error)
}

// SourceInputPluginContract is a REST wrapper around the constructor.ProcessSource interface for communicating
// with a plugin.
type SourceInputPluginContract interface {
	contracts.PluginBase
	IdentityProvider[runtime.Typed]
	ProcessSource(ctx context.Context, request *ProcessSourceInputRequest, credentials map[string]string) (*ProcessSourceInputResponse, error)
}

// InputPluginContract is used by the input registry to bundle together plugins of type ResourceInput and SourceInput.
type InputPluginContract interface {
	ResourceInputPluginContract
	SourceInputPluginContract
}
