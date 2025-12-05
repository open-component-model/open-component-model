package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// +k8s:deepcopy-gen=true
type TransformationGraphDefinition struct {
	Environment     *runtime.Unstructured   `json:"environment"`
	Transformations []GenericTransformation `json:"transformations"`
}

func (tgd *TransformationGraphDefinition) GetEnvironmentData() map[string]interface{} {
	if tgd.Environment == nil {
		return nil
	}
	return tgd.Environment.Data
}

// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type GenericTransformation struct {
	meta.TransformationMeta `json:",inline"`
	Spec                    *runtime.Unstructured `json:"spec"`
}
