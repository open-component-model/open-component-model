package v1

import (
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const ComponentVersionRepositoryPluginType types.PluginType = "componentVersionRepository"

var Scheme *runtime.Scheme

func init() {
	Scheme = runtime.NewScheme()
	Scheme.MustRegisterWithAlias(&CapabilitySpec{}, runtime.NewUnversionedType(string(ComponentVersionRepositoryPluginType)))
}

// CapabilitySpec specifies the supported types of a plugin for
// a particular capability type.
// For the ComponentVersionRepository capability, it specifies the supported
// repository spec types (e.g. oci, ctf, ...).
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type CapabilitySpec struct {
	// Type is the Capability Type of the plugin (e.g. ComponentVersionRepository).
	Type runtime.Type `json:"type"`
	// TypeToJSONSchema is a map, mapping type names to their JSON schema definitions.
	// It is implemented as a separate map instead of embedding it in types.Type
	// to avoid having to duplicate the schema in cases where plugins may have
	// input and output types that are the same.
	TypeToJSONSchema map[string][]byte `json:"typeToJSONSchema"`
	// Aliases map[string]runtime.Type `json:"aliases"` // mapping of canonical type (oci/v1alpha) to alias types (oci, oci/v1)
	// GetComponentVersion only has a dynamic input type (repository spec) and a fixed output type (descriptor).
	// So, we do not have to create a mapping from input to output types here.
	SupportedRepositorySpecTypes []types.Type `json:"supportedRepositorySpecTypes"`
}
