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

func (r *TypeToUntypedPlugin[T]) GetLocalResource(ctx context.Context, request v1.GetLocalResourceRequest[runtime.Typed], credentials map[string]string) (v1.GetLocalResourceResponse, error) {
	return r.base.GetLocalResource(ctx, v1.GetLocalResourceRequest[T]{
		Repository: request.Repository.(T),
		Name:       request.Name,
		Version:    request.Version,
		Identity:   request.Identity,
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

func (r *TypeToUntypedPlugin[T]) GetLocalSource(ctx context.Context, request v1.GetLocalSourceRequest[runtime.Typed], credentials map[string]string) (v1.GetLocalSourceResponse, error) {
	return r.base.GetLocalSource(ctx, v1.GetLocalSourceRequest[T]{
		Repository: request.Repository.(T),
		Name:       request.Name,
		Version:    request.Version,
		Identity:   request.Identity,
	}, credentials)
}

func (r *TypeToUntypedPlugin[T]) AddLocalSource(ctx context.Context, request v1.PostLocalSourceRequest[runtime.Typed], credentials map[string]string) (*descriptor.Source, error) {
	return r.base.AddLocalSource(ctx, v1.PostLocalSourceRequest[T]{
		Repository:     request.Repository.(T),
		Name:           request.Name,
		Version:        request.Version,
		SourceLocation: request.SourceLocation,
		Source:         request.Source,
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

func (r *TypeToUntypedPlugin[T]) ListComponentVersions(ctx context.Context, request v1.ListComponentVersionsRequest[runtime.Typed], credentials map[string]string) ([]string, error) {
	req := v1.ListComponentVersionsRequest[T]{
		Repository: request.Repository.(T),
		Name:       request.Name,
	}
	return r.base.ListComponentVersions(ctx, req, credentials)
}

func (r *TypeToUntypedPlugin[T]) GetIdentity(ctx context.Context, typ *v1.GetIdentityRequest[runtime.Typed]) (*v1.GetIdentityResponse, error) {
	return r.base.GetIdentity(ctx, &v1.GetIdentityRequest[T]{
		Typ: typ.Typ.(T),
	})
}
