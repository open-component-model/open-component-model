package v1alpha1

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const CTFAddComponentVersionType = "CTFAddComponentVersion"

// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type CTFAddComponentVersion struct {
	// +ocm:jsonschema-gen:enum=CTFAddComponentVersion/v1alpha1
	Type   runtime.Type                  `json:"type"`
	ID     string                        `json:"id,omitempty"`
	Spec   *CTFAddComponentVersionSpec   `json:"spec"`
	Output *CTFAddComponentVersionOutput `json:"output,omitempty"`
}

// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type CTFAddComponentVersionOutput struct{}

// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type CTFAddComponentVersionSpec struct {
	Repository ctf.Repository `json:"repository"`
	Descriptor *v2.Descriptor `json:"descriptor"`
}
