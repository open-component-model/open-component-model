package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const CTFAddOCIArtifactType = "CTFAddOCIArtifact"

// CTFAddOCIArtifact is a transformer specification to add an OCI artifact
// to a CTF repository.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type CTFAddOCIArtifact struct {
	// +ocm:jsonschema-gen:enum=CTFAddOCIArtifact/v1alpha1
	Type   runtime.Type             `json:"type"`
	ID     string                   `json:"id"`
	Spec   *CTFAddOCIArtifactSpec   `json:"spec"`
	Output *CTFAddOCIArtifactOutput `json:"output,omitempty"`
}

// CTFAddOCIArtifactOutput is the output specification of the
// CTFAddOCIArtifact transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type CTFAddOCIArtifactOutput struct {
	// Resource is the updated resource descriptor
	Resource *v2.Resource `json:"resource"`
}

// CTFAddOCIArtifactSpec is the input specification for the
// CTFAddOCIArtifact transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type CTFAddOCIArtifactSpec struct {
	// Repository is the CTF repository specification
	Repository ctf.Repository `json:"repository"`
	// Component is the component name
	Component string `json:"component"`
	// Version is the component version
	Version string `json:"version"`
	// Resource is the resource descriptor to add.
	Resource *v2.Resource `json:"resource"`
	// File is the access specification to the file containing the OCI artifact
	File v1alpha1.File `json:"file"`
}
