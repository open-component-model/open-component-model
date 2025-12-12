package v1

import (
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const BlobTransformerPluginType types.PluginType = "blobTransformer"

var Scheme *runtime.Scheme

func init() {
	Scheme = runtime.NewScheme()
	Scheme.MustRegisterWithAlias(&CapabilitySpec{}, runtime.NewUnversionedType(string(BlobTransformerPluginType)))
}

// CapabilitySpec specifies the supported types of a plugin for
// a particular capability type.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type CapabilitySpec struct {
	Type runtime.Type `json:"type"`
	// TODO(fabianburth): customize / optimize for blob transformers
	//  currently, it uses the general types.Type, but we might want to tailor this
	//  to blob transformers specifically.
	SupportedTransformerSpecTypes []types.Type `json:"supportedTransformerSpecTypes"`
}
