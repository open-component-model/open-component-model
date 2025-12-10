package ctf

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const DownloadComponentTransformationType = "ocm.software.download.component.ctf"

// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type DownloadComponentTransformation struct {
	// +ocm:jsonschema-gen:enum=ocm.software.download.component.ctf/v1alpha1
	Type   runtime.Type                           `json:"type"`
	ID     string                                 `json:"id,omitempty"`
	Spec   *DownloadComponentTransformationSpec   `json:"spec"`
	Output *DownloadComponentTransformationOutput `json:"output,omitempty"`
}

// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type DownloadComponentTransformationOutput struct {
	Descriptor *v2.Descriptor `json:"descriptor"`
}

// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type DownloadComponentTransformationSpec struct {
	Repository ctf.Repository `json:"repository"`
	Component  string         `json:"component"`
	Version    string         `json:"version"`
}
