package manager

import (
	"context"
	"ocm.software/open-component-model/bindings/go/runtime"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// EmptyBasePlugin can be used by internal implementations to skip having to implement
// the Ping method which will not be called anyway.
type EmptyBasePlugin struct{}

func (*EmptyBasePlugin) Ping(_ context.Context) error {
	return nil
}

// PluginBase is a capability shared by all plugins.
type PluginBase interface {
	// Ping makes sure the plugin is responsive.
	Ping(ctx context.Context) error
}

type Attribute string

type Attributes map[string]Attribute

type KV struct {
	Key   string
	Value string
}

// ReadOCMRepositoryPluginContract is a plugin type that can deal with repositories
// These provide type safety for all implementations. The Type defines the repository on which these requests work on.
type ReadOCMRepositoryPluginContract[T runtime.Typed] interface {
	PluginBase
	GetComponentVersion(ctx context.Context, request GetComponentVersionRequest[T], credentials Attributes) (*descriptor.Descriptor, error)
	GetLocalResource(ctx context.Context, request GetLocalResourceRequest[T], credentials Attributes) error
}

type WriteOCMRepositoryPluginContract[T runtime.Typed] interface {
	PluginBase
	AddLocalResource(ctx context.Context, request PostLocalResourceRequest[T], credentials Attributes) (*descriptor.Resource, error)
	AddComponentVersion(ctx context.Context, request PostComponentVersionRequest[T], credentials Attributes) error
}

type ReadWriteOCMRepositoryPluginContract[T runtime.Typed] interface {
	ReadOCMRepositoryPluginContract[T]
	WriteOCMRepositoryPluginContract[T]
}

type ResourcePluginContract interface {
	PluginBase
	AddGlobalResource(ctx context.Context, request PostResourceRequest, credentials Attributes) (*descriptor.Resource, error)
	GetGlobalResource(ctx context.Context, request GetResourceRequest, credentials Attributes) error
}

type ReadResourcePluginContract interface {
	PluginBase
	GetGlobalResource(ctx context.Context, request GetResourceRequest, credentials Attributes) error
}

type WriteResourcePluginContract interface {
	PluginBase
	AddGlobalResource(ctx context.Context, request PostResourceRequest, credentials Attributes) (*descriptor.Resource, error)
}
