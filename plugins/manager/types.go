package manager

import (
	"encoding/json"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	ReadWriteComponentVersionRepositoryCapability = "readWriteComponentVersionRepository"
	ReadComponentVersionRepositoryCapability      = "readComponentVersionRepository"
	WriteComponentVersionRepositoryCapability     = "writeComponentVersionRepository"
	ReadResourceRepositoryCapability              = "readResourceRepository"
	WriteResourceRepositoryCapability             = "writeResourceRepository"
)

type Location struct {
	LocationType `json:"type"`
	Value        string `json:"value"`
}

type Repository struct {
	runtime.Typed `json:",inline"`
}

func (a *Repository) UnmarshalJSON(data []byte) error {
	raw := &runtime.Raw{}
	if err := json.Unmarshal(data, raw); err != nil {
		return err
	}
	a.Typed = raw
	return nil
}

func (a *Repository) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.Typed)
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

type GetComponentVersionRequest struct {
	// The Location of the Component Version
	Repository *Repository `json:"repository"`
	// The Component Name
	Name string `json:"name"`
	// The Component Version
	Version string `json:"version"`
}

type PostComponentVersionRequest struct {
	Repository *Repository            `json:"repository"`
	Descriptor *descriptor.Descriptor `json:"descriptor"`
}

type GetLocalResourceRequest struct {
	// The Repository Specification where the Component Version is stored
	Repository *Repository `json:"repository"`
	// The Component Name
	Name string `json:"name"`
	// The Component Version
	Version string `json:"version"`

	// Identity of the local resource
	Identity map[string]string `json:"identity,omitempty"`

	// The Location of the Local Resource to download to
	TargetLocation Location `json:"targetLocation"`
}

type PostLocalResourceRequest struct {
	// The Repository Specification where the Component Version should be stored
	Repository *Repository `json:"repository"`
	// The Component Name
	Name string `json:"name"`
	// The Component Version
	Version string `json:"version"`

	// The ResourceLocation of the Local Resource
	ResourceLocation Location             `json:"resourceLocation"`
	Resource         *descriptor.Resource `json:"resource"`
}

// Endpoint defines an access point like /cv/upload, /cv/download etc.
type Endpoint struct {
	// Path defines the access location of an endpoint.
	Path string `json:"path"`
	// Schema is optional as some endpoints don't define a schema.
	Schema []byte `json:"scheme,omitempty"`
}

// Capability defines a capability which consists of an Access Type and several endpoints.
type Capability struct {
	// Capability is the name of the capability for example OCMComponentVersionRepository.
	Capability string `json:"capability"`
	// Type defines the type name that this plugin supports.
	Type string `json:"type"`
}

type Capabilities struct {
	PluginType   PluginType   `json:"pluginType"`
	Capabilities []Capability `json:"capabilities"` // is it multiple capabilities? Maybe.
}

type GetResourceRequest struct {
	Location
	// The resource specification to download
	*descriptor.Resource `json:"resource"`

	// The Location of the Local Resource to download to
	TargetLocation Location `json:"targetLocation"`
}

type PostResourceRequest struct {
	//TargetAccess *transfer.Access `json:"targetAccess"`
	// The ResourceLocation of the Local Resource
	ResourceLocation Location             `json:"resourceLocation"`
	Resource         *descriptor.Resource `json:"resource"`
}
