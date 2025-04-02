package manager

import (
	"context"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

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

type GenericPluginContract interface {
	PluginBase
	Call(ctx context.Context, endpoint, method string, payload, response any, headers []KV, params []KV) error
}

// ReadRepositoryPluginContract is a plugin type that can deal with repositories
type ReadRepositoryPluginContract interface {
	PluginBase
	GetComponentVersion(ctx context.Context, request GetComponentVersionRequest, credentials Attributes) (*descriptor.Descriptor, error)
	GetLocalResource(ctx context.Context, request GetLocalResourceRequest, credentials Attributes) error
}

type WriteRepositoryPluginContract interface {
	PluginBase
	AddLocalResource(ctx context.Context, request PostLocalResourceRequest, credentials Attributes) (*descriptor.Resource, error)
	AddComponentVersion(ctx context.Context, request PostComponentVersionRequest, credentials Attributes) error
}

type ReadWriteRepositoryPluginContract interface {
	ReadRepositoryPluginContract
	WriteRepositoryPluginContract
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

//type CredentialPluginContract interface {
//	PluginBase
//	credentials.CredentialPlugin
//}
//
//type CredentialRepositoryPluginContract interface {
//	PluginBase
//	credentials.RepositoryPlugin
//}
//
//type TransformerPluginContract interface {
//	PluginBase
//	CredentialIdentities(ctx context.Context, request CredentialIdentityRequest) (*CredentialIdentityResponse, error)
//	Transform(ctx context.Context, request TransformResourceRequest) (*TransformResourceResponse, error)
//}
