package v1alpha1

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const CTFGetComponentVersionType = "CTFGetComponentVersion"

// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type CTFGetComponentVersion struct {
	// +ocm:jsonschema-gen:enum=CTFGetComponentVersion/v1alpha1
	Type   runtime.Type                  `json:"type"`
	ID     string                        `json:"id,omitempty"`
	Spec   *CTFGetComponentVersionSpec   `json:"spec"`
	Output *CTFGetComponentVersionOutput `json:"output,omitempty"`
}

// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type CTFGetComponentVersionOutput struct {
	Descriptor *v2.Descriptor `json:"descriptor"`
}

// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type CTFGetComponentVersionSpec struct {
	Repository ctf.Repository `json:"repository"`
	Component  string         `json:"component"`
	Version    string         `json:"version"`
}
