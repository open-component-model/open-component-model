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

type ResourceInputPluginContract interface {
	contracts.PluginBase
	IdentityProvider[runtime.Typed]
	ProcessResource(ctx context.Context, request *ProcessResourceInputRequest, credentials map[string]string) (*ProcessResourceResponse, error)
}

type SourceInputPluginContract interface {
	contracts.PluginBase
	IdentityProvider[runtime.Typed]
	ProcessSource(ctx context.Context, request *ProcessSourceInputRequest, credentials map[string]string) (*ProcessSourceResponse, error)
}

type InputPluginContract interface {
	ResourceInputPluginContract
	SourceInputPluginContract
}
