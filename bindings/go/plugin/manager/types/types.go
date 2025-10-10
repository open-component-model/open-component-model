package types

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// PluginType defines the type of the plugin such as, ComponentVersionRepositoryPlugin, Transformation, Credential, Config plugin.
type PluginType string

var (
	ComponentVersionRepositoryPluginType PluginType = "componentVersionRepository"
	CredentialRepositoryPluginType       PluginType = "credentialRepository" //nolint:gosec // not hardcoded cred
	InputPluginType                      PluginType = "inputRepository"
	DigestProcessorPluginType            PluginType = "digestProcessorRepository"
	ResourceRepositoryPluginType         PluginType = "resourceRepository"
	BlobTransformerPluginType            PluginType = "blobTransformer"
	SigningHandlerPluginType             PluginType = "signingHandler"
	ComponentListerPluginType            PluginType = "componentLister"
)

type Location struct {
	LocationType `json:"type"`
	Value        string `json:"value"`
	MediaType    string `json:"mediaType,omitempty"`
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

// Type defines an endpoint's type and the scheme of the type.
type Type struct {
	// Type defines the type name that this plugin supports.
	Type runtime.Type `json:"type"`
	// JSONScheme holds the scheme for the type. This scheme corresponds to the type.
	JSONSchema []byte `json:"jsonSchema"`
}

// Types contains all the types a specific plugin has declared for a specific functionality.
type Types struct {
	// Types define a plugin type specific list of types that the plugin supports.
	Types map[PluginType][]Type `json:"types"`
	// ConfigTypes define a list of configuration types the plugin understands. These will be provided during startup.
	ConfigTypes []runtime.Type `json:"configTypes,omitempty"`
}
