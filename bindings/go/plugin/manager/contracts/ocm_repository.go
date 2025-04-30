package contracts

import (
	"context"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ReadOCMRepositoryPluginContract is a plugin type that can deal with repositories
// These provide type safety for all implementations. The Type defines the repository on which these requests work on.
type ReadOCMRepositoryPluginContract[T runtime.Typed] interface {
	PluginBase
	GetComponentVersion(ctx context.Context, request types.GetComponentVersionRequest[T], credentials map[string]string) (*descriptor.Descriptor, error)
	GetLocalResource(ctx context.Context, request types.GetLocalResourceRequest[T], credentials map[string]string) error
}

// WriteOCMRepositoryPluginContract defines the ability to upload ComponentVersions to a repository with a given Type.
type WriteOCMRepositoryPluginContract[T runtime.Typed] interface {
	PluginBase
	AddLocalResource(ctx context.Context, request types.PostLocalResourceRequest[T], credentials map[string]string) (*descriptor.Resource, error)
	AddComponentVersion(ctx context.Context, request types.PostComponentVersionRequest[T], credentials map[string]string) error
}

// ReadWriteOCMRepositoryPluginContract is a combination of Read and Write contract.
type ReadWriteOCMRepositoryPluginContract[T runtime.Typed] interface {
	ReadOCMRepositoryPluginContract[T]
	WriteOCMRepositoryPluginContract[T]
}

// ResourcePluginContract is the contract defining Add and Get global resources.
type ResourcePluginContract interface {
	PluginBase
	AddGlobalResource(ctx context.Context, request types.PostResourceRequest, credentials map[string]string) (*descriptor.Resource, error)
	GetGlobalResource(ctx context.Context, request types.GetResourceRequest, credentials map[string]string) error
}

type ReadResourcePluginContract interface {
	PluginBase
	GetGlobalResource(ctx context.Context, request types.GetResourceRequest, credentials map[string]string) error
}

type WriteResourcePluginContract interface {
	PluginBase
	AddGlobalResource(ctx context.Context, request types.PostResourceRequest, credentials map[string]string) (*descriptor.Resource, error)
}
