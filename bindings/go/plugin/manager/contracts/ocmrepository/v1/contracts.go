package v1

import (
	"context"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// IdentityProvider provides a way to retrieve the identity of a plugin. This identity can then further be used to resolve
// credentials for a specific plugin.
type IdentityProvider[T runtime.Typed] interface {
	contracts.PluginBase
	GetIdentity(ctx context.Context, typ *GetIdentityRequest[T]) (*GetIdentityResponse, error)
}

// ReadOCMRepositoryPluginContract is a plugin type that can deal with repositories
// These provide type safety for all implementations. The Type defines the repository on which these requests work on.
type ReadOCMRepositoryPluginContract[T runtime.Typed] interface {
	contracts.PluginBase
	IdentityProvider[T]
	HealthCheckable[T]
	GetComponentVersion(ctx context.Context, request GetComponentVersionRequest[T], credentials runtime.Typed) (*descriptor.Descriptor, error)
	ListComponentVersions(ctx context.Context, request ListComponentVersionsRequest[T], credentials runtime.Typed) ([]string, error)
	GetLocalResource(ctx context.Context, request GetLocalResourceRequest[T], credentials runtime.Typed) (GetLocalResourceResponse, error)
	GetLocalSource(ctx context.Context, request GetLocalSourceRequest[T], credentials runtime.Typed) (GetLocalSourceResponse, error)
}

// WriteOCMRepositoryPluginContract defines the ability to upload ComponentVersions to a repository with a given Type.
type WriteOCMRepositoryPluginContract[T runtime.Typed] interface {
	contracts.PluginBase
	IdentityProvider[T]
	AddLocalResource(ctx context.Context, request PostLocalResourceRequest[T], credentials runtime.Typed) (*descriptor.Resource, error)
	AddLocalSource(ctx context.Context, request PostLocalSourceRequest[T], credentials runtime.Typed) (*descriptor.Source, error)
	AddComponentVersion(ctx context.Context, request PostComponentVersionRequest[T], credentials runtime.Typed) error
}

// ReadWriteOCMRepositoryPluginContract is a combination of Read and Write contract.
type ReadWriteOCMRepositoryPluginContract[T runtime.Typed] interface {
	ReadOCMRepositoryPluginContract[T]
	WriteOCMRepositoryPluginContract[T]
}

// HealthCheckable is an optional interface that can be implemented by a
// component version repository.
type HealthCheckable[T runtime.Typed] interface {
	CheckHealth(ctx context.Context, request PostCheckHealthRequest[T], credentials runtime.Typed) error
}
