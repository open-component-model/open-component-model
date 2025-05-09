package v1

import (
	"context"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ReadOCMRepositoryPluginContract is a plugin type that can deal with repositories
// These provide type safety for all implementations. The Type defines the repository on which these requests work on.
type ReadOCMRepositoryPluginContract[T runtime.Typed] interface {
	contracts.PluginBase
	IdentityProvider[T]
	GetComponentVersion(ctx context.Context, request GetComponentVersionRequest[T], credentials map[string]string) (*descriptor.Descriptor, error)
	GetLocalResource(ctx context.Context, request GetLocalResourceRequest[T], credentials map[string]string) error
}

// WriteOCMRepositoryPluginContract defines the ability to upload ComponentVersions to a repository with a given Type.
type WriteOCMRepositoryPluginContract[T runtime.Typed] interface {
	contracts.PluginBase
	IdentityProvider[T]
	AddLocalResource(ctx context.Context, request PostLocalResourceRequest[T], credentials map[string]string) (*descriptor.Resource, error)
	AddComponentVersion(ctx context.Context, request PostComponentVersionRequest[T], credentials map[string]string) error
}

// ReadWriteOCMRepositoryPluginContract is a combination of Read and Write contract.
type ReadWriteOCMRepositoryPluginContract[T runtime.Typed] interface {
	ReadOCMRepositoryPluginContract[T]
	WriteOCMRepositoryPluginContract[T]
}

// ResourcePluginContract is the contract defining Add and Get global resources.
type ResourcePluginContract interface {
	contracts.PluginBase
	AddGlobalResource(ctx context.Context, request PostResourceRequest, credentials map[string]string) (*descriptor.Resource, error)
	GetGlobalResource(ctx context.Context, request GetResourceRequest, credentials map[string]string) error
}

type ReadResourcePluginContract interface {
	contracts.PluginBase
	GetGlobalResource(ctx context.Context, request GetResourceRequest, credentials map[string]string) error
}

type WriteResourcePluginContract interface {
	contracts.PluginBase
	AddGlobalResource(ctx context.Context, request PostResourceRequest, credentials map[string]string) (*descriptor.Resource, error)
}

type IdentityProvider[T runtime.Typed] interface {
	contracts.PluginBase
	GetIdentity(ctx context.Context, typ GetIdentityRequest[T]) (runtime.Identity, error)
}
