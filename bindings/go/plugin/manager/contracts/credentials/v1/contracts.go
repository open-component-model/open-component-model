package v1

import (
	"context"

	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type CredentialRepositoryPluginContract[T runtime.Typed] interface {
	contracts.PluginBase
	ConsumerIdentityForConfig(ctx context.Context, cfg ConsumerIdentityForConfigRequest[T]) (runtime.Identity, error)
	Resolve(ctx context.Context, cfg ResolveRequest[T], credentials map[string]string) (map[string]string, error)
}
