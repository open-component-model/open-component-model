package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	AddOCIArtifactType    = "AddOCIArtifact"
	AddOCIArtifactVersion = "v1alpha1"
)

// AddOCIArtifact is a transformation that uploads OCI artifacts to remote oci registries.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type AddOCIArtifact struct {
	// +ocm:jsonschema-gen:enum=AddOCIArtifact/v1alpha1
	Type   runtime.Type          `json:"type"`
	ID     string                `json:"id"`
	Spec   *AddOCIArtifactSpec   `json:"spec"`
	Output *AddOCIArtifactOutput `json:"output,omitempty"`
}

// AddOCIArtifactSpec is the input specification for the
// AddOCIArtifact transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type AddOCIArtifactSpec struct {
	// Resource is the resource descriptor to add.
	// If the Resource contains an access specification, it may be used
	// by the underlying implementation to derive metadata to avoid additional compute
	// (such as digest information) or to steer implementation (such as a reference name)
	Resource *v2.Resource `json:"resource"`
	// File is the access specification to the file that should be added
	File v1alpha1.File `json:"file"`
}

// AddOCIArtifactOutput is the output specification for the
// AddOCIArtifact transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type AddOCIArtifactOutput struct {
	// Resource is the updated resource descriptor with complete oci image reference
	Resource *v2.Resource `json:"resource"`
}
