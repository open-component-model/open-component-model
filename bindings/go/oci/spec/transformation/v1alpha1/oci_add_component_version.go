package v1alpha1

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const OCIAddComponentVersionType = "OCIAddComponentVersion"

// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type OCIAddComponentVersion struct {
	// +ocm:jsonschema-gen:enum=OCIAddComponentVersion/v1alpha1
	Type   runtime.Type                  `json:"type"`
	ID     string                        `json:"id,omitempty"`
	Spec   *OCIAddComponentVersionSpec   `json:"spec"`
	Output *OCIAddComponentVersionOutput `json:"output,omitempty"`
}

// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type OCIAddComponentVersionOutput struct{}

// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type OCIAddComponentVersionSpec struct {
	Repository oci.Repository `json:"repository"`
	Descriptor *v2.Descriptor `json:"descriptor"`
}
