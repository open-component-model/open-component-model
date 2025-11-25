package v1

import (
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const InputPluginType types.PluginType = "inputRepository"

var Scheme *runtime.Scheme

func init() {
	Scheme = runtime.NewScheme()
	Scheme.MustRegisterWithAlias(&CapabilitySpec{}, runtime.NewUnversionedType(string(InputPluginType)))
}

// CapabilitySpec specifies the supported types of a plugin for
// a particular capability type.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type CapabilitySpec struct {
	Type                runtime.Type      `json:"type"`
	TypeToJSONSchema    map[string][]byte `json:"typeToJSONSchema"`
	SupportedInputTypes []types.Type      `json:"supportedInputTypes"`
}
