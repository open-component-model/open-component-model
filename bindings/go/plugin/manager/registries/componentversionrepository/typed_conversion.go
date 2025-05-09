package componentversionrepository

import (
	"context"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type TypeToUntypedPlugin[T runtime.Typed] struct {
	base v1.ReadWriteOCMRepositoryPluginContract[T]
}

var _ v1.ReadWriteOCMRepositoryPluginContract[runtime.Typed] = &TypeToUntypedPlugin[runtime.Typed]{}

func (r *TypeToUntypedPlugin[T]) Ping(ctx context.Context) error {
	return r.base.Ping(ctx)
}

func (r *TypeToUntypedPlugin[T]) GetLocalResource(ctx context.Context, request v1.GetLocalResourceRequest[runtime.Typed], credentials map[string]string) error {
	return r.base.GetLocalResource(ctx, v1.GetLocalResourceRequest[T]{
		Repository:     request.Repository.(T),
		Name:           request.Name,
		Version:        request.Version,
		Identity:       request.Identity,
		TargetLocation: request.TargetLocation,
	}, credentials)
}

func (r *TypeToUntypedPlugin[T]) AddLocalResource(ctx context.Context, request v1.PostLocalResourceRequest[runtime.Typed], credentials map[string]string) (*descriptor.Resource, error) {
	return r.base.AddLocalResource(ctx, v1.PostLocalResourceRequest[T]{
		Repository:       request.Repository.(T),
		Name:             request.Name,
		Version:          request.Version,
		ResourceLocation: request.ResourceLocation,
		Resource:         request.Resource,
	}, credentials)
}

func (r *TypeToUntypedPlugin[T]) AddComponentVersion(ctx context.Context, request v1.PostComponentVersionRequest[runtime.Typed], credentials map[string]string) error {
	return r.base.AddComponentVersion(ctx, v1.PostComponentVersionRequest[T]{
		Repository: request.Repository.(T),
		Descriptor: request.Descriptor,
	}, credentials)
}

func (r *TypeToUntypedPlugin[T]) GetComponentVersion(ctx context.Context, request v1.GetComponentVersionRequest[runtime.Typed], credentials map[string]string) (*descriptor.Descriptor, error) {
	req := v1.GetComponentVersionRequest[T]{
		Repository: request.Repository.(T),
		Name:       request.Name,
		Version:    request.Version,
	}
	return r.base.GetComponentVersion(ctx, req, credentials)
}

func (r *TypeToUntypedPlugin[T]) GetIdentity(ctx context.Context, typ v1.GetIdentityRequest[runtime.Typed]) (runtime.Identity, error) {
	return r.base.GetIdentity(ctx, v1.GetIdentityRequest[T]{
		Typ: typ.Typ.(T),
	})
}
