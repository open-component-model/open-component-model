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
	Output                  *runtime.Unstructured `json:"output,omitempty"`
}

func (t *GenericTransformation) AsRaw() *runtime.Raw {
	var r runtime.Raw
	if err := runtime.NewScheme(runtime.WithAllowUnknown()).Convert(t, &r); err != nil {
		panic(err)
	}
	return &r
}

func (t *GenericTransformation) AsUnstructured() *runtime.Unstructured {
	obj := map[string]interface{}{
		"id":   t.ID,
		"type": t.GetType().String(),
	}
	if t.Spec != nil {
		obj["spec"] = t.Spec.Data
	}
	if t.Output != nil {
		obj["output"] = t.Output.Data
	}
	return &runtime.Unstructured{Data: obj}
}

func GenericTransformationFromTyped(r runtime.Typed) (*GenericTransformation, error) {
	// Convert to raw first to drop unknown fields
	var t runtime.Raw
	if err := runtime.NewScheme(runtime.WithAllowUnknown()).Convert(r, &t); err != nil {
		return nil, err
	}
	var gt GenericTransformation
	if err := runtime.NewScheme(runtime.WithAllowUnknown()).Convert(&t, &gt); err != nil {
		return nil, err
	}
	return &gt, nil
}
