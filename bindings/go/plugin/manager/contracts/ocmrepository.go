package contracts

import (
	"context"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type Attribute string

type Attributes map[string]Attribute

// ReadOCMRepositoryPluginContract is a plugin type that can deal with repositories
// These provide type safety for all implementations. The Type defines the repository on which these requests work on.
type ReadOCMRepositoryPluginContract[T runtime.Typed] interface {
	PluginBase
	GetComponentVersion(ctx context.Context, request types.GetComponentVersionRequest[T], credentials Attributes) (*descriptor.Descriptor, error)
	GetLocalResource(ctx context.Context, request types.GetLocalResourceRequest[T], credentials Attributes) error
}

type WriteOCMRepositoryPluginContract[T runtime.Typed] interface {
	PluginBase
	AddLocalResource(ctx context.Context, request types.PostLocalResourceRequest[T], credentials Attributes) (*descriptor.Resource, error)
	AddComponentVersion(ctx context.Context, request types.PostComponentVersionRequest[T], credentials Attributes) error
}

type ReadWriteOCMRepositoryPluginContract[T runtime.Typed] interface {
	ReadOCMRepositoryPluginContract[T]
	WriteOCMRepositoryPluginContract[T]
}

type ResourcePluginContract interface {
	PluginBase
	AddGlobalResource(ctx context.Context, request types.PostResourceRequest, credentials Attributes) (*descriptor.Resource, error)
	GetGlobalResource(ctx context.Context, request types.GetResourceRequest, credentials Attributes) error
}

type ReadResourcePluginContract interface {
	PluginBase
	GetGlobalResource(ctx context.Context, request types.GetResourceRequest, credentials Attributes) error
}

type WriteResourcePluginContract interface {
	PluginBase
	AddGlobalResource(ctx context.Context, request types.PostResourceRequest, credentials Attributes) (*descriptor.Resource, error)
}
