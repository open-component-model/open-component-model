package types

import (
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// PluginType defines the type of the plugin such as, ComponentVersionRepositoryPlugin, Transformation, Credential, Config plugin.
type PluginType string

var ComponentVersionRepositoryPluginType PluginType = "componentVersionRepository"

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

// Type defines an endpoint's type and the scheme of the type.
type Type struct {
	// Type defines the type name that this plugin supports.
	Type runtime.Type `json:"type"`
	// JSONScheme holds the scheme for the type. This scheme corresponds to the type.
	JSONSchema []byte `json:"jsonSchema"`
}

// Types contains all the types a specific plugin has declared for a specific functionality.
type Types struct {
	// Maybe we don't even need the plugin type here?
	// Does a binary implement multiple plugin types? Didn't we say we don't want to overstep that boundary?
	Types map[PluginType][]Type `json:"types"`
}
