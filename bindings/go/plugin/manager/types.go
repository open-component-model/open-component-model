package manager

import (
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	ReadComponentVersionRepositoryCapability  = "readComponentVersionRepository"
	WriteComponentVersionRepositoryCapability = "writeComponentVersionRepository"
	ReadResourceRepositoryCapability          = "readResourceRepository"
	WriteResourceRepositoryCapability         = "writeResourceRepository"
	CredentialPluginCapability                = "credentialPlugin"           //nolint: gosec // isn't a hardcoded credential
	CredentialRepositoryPluginCapability      = "credentialRepositoryPlugin" //nolint: gosec // isn't a hardcoded credential
)

type Location struct {
	LocationType `json:"type"`
	Value        string `json:"value"`
}

type LocationType string

const (
	// LocationTypeRemoteURL is a remote URL available to the plugin only.
	// It MUST be a valid URL. It MAY be accessible to the orchestrator, but MUST be accessible to the plugin.
	// The URL SHOULD be protected with Credentials.
	LocationTypeRemoteURL LocationType = "remoteURL"
	// LocationTypeUnixNamedPipe is a Unix named pipe available to the orchestrator and plugin.
	// It MUST be an absolute path. It SHOULD be opened with O_NONBLOCK whenever reading from it.
	LocationTypeUnixNamedPipe LocationType = "unixNamedPipe"
	// LocationTypeLocalFile is a local file present on the filesystem available to the orchestrator and plugin.
	// It MUST be an absolute path.
	LocationTypeLocalFile LocationType = "localFile"
)

type GetComponentVersionRequest[T runtime.Typed] struct {
	// The Location of the Component Version
	Repository T `json:"repository"`
	// The Component Name
	Name string `json:"name"`
	// The Component Version
	Version string `json:"version"`
}

type PostComponentVersionRequest[T runtime.Typed] struct {
	Repository T                      `json:"repository"`
	Descriptor *descriptor.Descriptor `json:"descriptor"`
}

type GetLocalResourceRequest[T runtime.Typed] struct {
	// The Repository Specification where the Component Version is stored
	Repository T `json:"repository"`
	// The Component Name
	Name string `json:"name"`
	// The Component Version
	Version string `json:"version"`

	// Identity of the local resource
	Identity map[string]string `json:"identity,omitempty"`

	// The Location of the Local Resource to download to
	TargetLocation Location `json:"targetLocation"`
}

type PostLocalResourceRequest[T runtime.Typed] struct {
	// The Repository Specification where the Component Version should be stored
	Repository T `json:"repository"`
	// The Component Name
	Name string `json:"name"`
	// The Component Version
	Version string `json:"version"`

	// The ResourceLocation of the Local Resource
	ResourceLocation Location             `json:"resourceLocation"`
	Resource         *descriptor.Resource `json:"resource"`
}

// Type defines an endpoint's type and the scheme of the type.
type Type struct {
	// Type defines the type name that this plugin supports.
	Type runtime.Type `json:"type"`
	// JSONScheme holds the scheme for the type. This scheme corresponds to the type.
	JSONSchema []byte `json:"jsonSchema"`
}

// Endpoint defines a type and a scheme belonging to one of the N endpoints for a Capability.
type Endpoint struct {
	// Location of the endpoint. This location is used to identify which JsonSchema belongs
	// to which endpoint.
	Location string `json:"location"`
	// Types and endpoints stored in this capability.
	Types []Type `json:"types"`
}

// Capability defines a capability which consists of an Access Type and several endpoints.
type Capability struct {
	Name string `json:"name"` // or ID or something.
	// Endpoints has a list of endpoints that belong to this capability.
	Endpoints []Endpoint `json:"endpoints"`
}

// Capabilities are defined per plugin type. The plugin type is derived from the capability
// and set during generation of the capability.
type Capabilities struct {
	Capabilities map[PluginType][]Capability `json:"capabilities"`
}

type GetResourceRequest struct {
	Location
	// The resource specification to download
	*descriptor.Resource `json:"resource"`

	// The Location of the Local Resource to download to
	TargetLocation Location `json:"targetLocation"`
}

type PostResourceRequest struct {
	// The ResourceLocation of the Local Resource
	ResourceLocation Location             `json:"resourceLocation"`
	Resource         *descriptor.Resource `json:"resource"`
}
