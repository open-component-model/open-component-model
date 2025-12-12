package v1alpha1

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const CTFAddComponentVersionType = "CTFAddComponentVersion"

// CTFAddComponentVersion is a transformer specification to add a component
// version to a CTF repository.
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

// CTFAddComponentVersionOutput is the output specification of the
// CTFAddComponentVersion.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type CTFAddComponentVersionOutput struct{}

// CTFAddComponentVersionSpec is the specification of the input specification
// for the CTFAddComponentVersion.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type CTFAddComponentVersionSpec struct {
	Repository ctf.Repository `json:"repository"`
	Descriptor *v2.Descriptor `json:"descriptor"`
}
