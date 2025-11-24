package componentversionrepository

import (
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme *runtime.Scheme

func init() {
	Scheme = runtime.NewScheme()
	Scheme.MustRegisterWithAlias(&CapabilitySpec{}, runtime.NewUnversionedType(string(types.ComponentVersionRepositoryPluginType)))
}

// move to contracts (capabilities as well)

// CapabilitySpec
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
	SupportedRepositorySpecTypes []types.Type `json:"supportedRepositorySpecTypes"` // the list of types (oci, ctf, ...) supported for get
}

//
//func (c *CapabilitySpec) RegisterCapability(registry PluginRegistry) error {
//	if err := registry.RegisterComponentVersionRepositoryPlugin(c); err != nil {
//		return fmt.Errorf("failed to register component version repository plugin: %w", err)
//	}
//	return nil
//}
//
//type PluginRegistry interface {
//	RegisterComponentVersionRepositoryPlugin(spec *CapabilitySpec) error
//}
