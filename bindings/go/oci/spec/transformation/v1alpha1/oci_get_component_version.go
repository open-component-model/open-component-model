package v1alpha1

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const OCIGetComponentVersionType = "OCIGetComponentVersion"

// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type OCIGetComponentVersion struct {
	// +ocm:jsonschema-gen:enum=OCIGetComponentVersion/v1alpha1
	Type   runtime.Type                  `json:"type"`
	ID     string                        `json:"id,omitempty"`
	Spec   *OCIGetComponentVersionSpec   `json:"spec"`
	Output *OCIGetComponentVersionOutput `json:"output,omitempty"`
}

// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type OCIGetComponentVersionOutput struct {
	Descriptor *v2.Descriptor `json:"descriptor"`
}

// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type OCIGetComponentVersionSpec struct {
	Repository oci.Repository `json:"repository"`
	Component  string         `json:"component"`
	Version    string         `json:"version"`
}
