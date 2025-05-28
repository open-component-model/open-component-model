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

type ResourceInputPluginContract interface {
	contracts.PluginBase
	IdentityProvider[runtime.Typed]
	ProcessResource(ctx context.Context, request *ProcessResourceRequest, credentials map[string]string) (*ProcessResourceResponse, error)
}

type SourceInputPluginContract interface {
	contracts.PluginBase
	IdentityProvider[runtime.Typed]
	ProcessSource(ctx context.Context, request *ProcessSourceRequest, credentials map[string]string) (*ProcessSourceResponse, error)
}

type ResourceDigestProcessorPlugin interface {
	contracts.PluginBase
	ProcessResourceDigest(ctx context.Context, resource *descriptor.Resource) (*descriptor.Resource, error)
}

// ConstructionContract is used to store plugins that implement either of these functionalities.
// Further refinement is then possible via using a specific Get function.
type ConstructionContract interface {
	IdentityProvider[runtime.Typed]
	ResourceInputPluginContract
	SourceInputPluginContract
	ResourceDigestProcessorPlugin
}
