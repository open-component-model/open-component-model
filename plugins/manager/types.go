package manager

import (
	"encoding/json"

	v1 "ocm.software/open-component-model/bindings/go/credentials/config/v1"
	"ocm.software/open-component-model/bindings/go/descriptor"
	"ocm.software/open-component-model/bindings/go/runtime"
	transfer "ocm.software/open-component-model/bindings/go/transfer/spec"
)

const (
	ReadWriteComponentVersionRepositoryCapability = "readWriteComponentVersionRepository"
	ReadComponentVersionRepositoryCapability      = "readComponentVersionRepository"
	WriteComponentVersionRepositoryCapability     = "writeComponentVersionRepository"
	ReadResourceRepositoryCapability              = "readResourceRepository"
	WriteResourceRepositoryCapability             = "writeResourceRepository"
	GenericRepositoryCapability                   = "generic"
	CredentialPluginCapability                    = "credentialPlugin"
	CredentialRepositoryPluginCapability          = "credentialRepositoryPlugin"
	TransformerCapability                         = "transformer"
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

// Capability defines a capability and the schema it has to conform to.
type Capability struct {
	Capability string `json:"capability"`
	Schema     []byte `json:"schema"`
}

type Capabilities struct {
	// Type is a map of types with capabilities.
	Type map[runtime.Type][]Capability `json:"type"`
}

type GetResourceRequest struct {
	Location
	// The resource specification to download
	*descriptor.Resource `json:"resource"`

	// The Location of the Local Resource to download to
	TargetLocation Location `json:"targetLocation"`
}

type TransformResourceRequest struct {
	TransformationMeta `json:"transformationMeta"`

	// The resource specification to download
	*descriptor.Resource `json:"resource"`

	TransformationSpec *TransformationSpec `json:"transformSpec"`

	// The Location of the resource that should be localized
	ResourceLocation Location `json:"resourceLocation"`
	// The Location of the transformed resource
	TransformedResourceLocation Location `json:"transformedResourceLocation"`

	Inputs map[string]string `json:"inputs"`

	Credentials v1.Attributes `json:"credentials"`
}

type CredentialIdentityRequest struct {
	// The transformation that should be interpreted
	TransformResourceRequest `json:"transformResourceRequest"`
}

type CredentialIdentityResponse struct {
	// The credential identities that can be used for transformation
	Identities []v1.Identity `json:"identities"`
}

type TransformationMeta struct {
	ComponentIdentity descriptor.ComponentIdentity
	Source            *TransformationRepository `json:"source"`
	Target            *TransformationRepository `json:"target"`
}

type TransformationRepository struct {
	runtime.Typed `json:",inline"`
}

func (a *TransformationRepository) UnmarshalJSON(data []byte) error {
	raw := &runtime.Raw{}
	if err := json.Unmarshal(data, raw); err != nil {
		return err
	}
	a.Typed = raw
	return nil
}

func (a *TransformationRepository) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.Typed)
}

type TransformationSpec struct {
	runtime.Typed `json:",inline"`
}

func (a *TransformationSpec) UnmarshalJSON(data []byte) error {
	raw := &runtime.Raw{}
	if err := json.Unmarshal(data, raw); err != nil {
		return err
	}
	a.Typed = raw
	return nil
}

func (a *TransformationSpec) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.Typed)
}

type TransformResourceResponse struct {
	// The resource specification to download
	*descriptor.Resource `json:"resource"`

	Outputs map[string]string `json:"outputs"`
}

type PostResourceRequest struct {
	TargetAccess *transfer.Access `json:"targetAccess"`
	// The ResourceLocation of the Local Resource
	ResourceLocation Location             `json:"resourceLocation"`
	Resource         *descriptor.Resource `json:"resource"`
}
