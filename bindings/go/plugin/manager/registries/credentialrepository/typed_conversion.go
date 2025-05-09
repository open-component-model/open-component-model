package credentialrepository

import (
	"context"

	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type TypeToUntypedPlugin[T runtime.Typed] struct {
	base v1.CredentialRepositoryPluginContract[T]
}

var _ v1.CredentialRepositoryPluginContract[runtime.Typed] = &TypeToUntypedPlugin[runtime.Typed]{}

func (r *TypeToUntypedPlugin[T]) ConsumerIdentityForConfig(ctx context.Context, cfg v1.ConsumerIdentityForConfigRequest[runtime.Typed]) (runtime.Identity, error) {
	return r.base.ConsumerIdentityForConfig(ctx, v1.ConsumerIdentityForConfigRequest[T]{
		Config: cfg.Config.(T),
	})
}

func (r *TypeToUntypedPlugin[T]) Resolve(ctx context.Context, cfg v1.ResolveRequest[runtime.Typed], credentials map[string]string) (map[string]string, error) {
	return r.base.Resolve(ctx, v1.ResolveRequest[T]{
		Config:   cfg.Config.(T),
		Identity: cfg.Identity,
	}, credentials)
}

func (r *TypeToUntypedPlugin[T]) Ping(ctx context.Context) error {
	return r.base.Ping(ctx)
}
