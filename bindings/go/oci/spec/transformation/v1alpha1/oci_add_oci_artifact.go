package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const OCIAddOCIArtifactType = "OCIAddOCIArtifact"

// OCIAddOCIArtifact is a transformer specification to upload an OCI artifact
// to a target OCI registry and update the resource's access specification.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type OCIAddOCIArtifact struct {
	// +ocm:jsonschema-gen:enum=OCIAddOCIArtifact/v1alpha1
	Type   runtime.Type             `json:"type"`
	ID     string                   `json:"id"`
	Spec   *OCIAddOCIArtifactSpec   `json:"spec"`
	Output *OCIAddOCIArtifactOutput `json:"output,omitempty"`
}

// OCIAddOCIArtifactOutput is the output specification of the
// OCIAddOCIArtifact transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type OCIAddOCIArtifactOutput struct {
	// Resource is the updated resource descriptor with new imageReference
	Resource *v2.Resource `json:"resource"`
}

// OCIAddOCIArtifactSpec is the input specification for the
// OCIAddOCIArtifact transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type OCIAddOCIArtifactSpec struct {
	// Repository is the OCI repository specification
	Repository oci.Repository `json:"repository"`
	// Component is the component name
	Component string `json:"component"`
	// Version is the component version
	Version string `json:"version"`
	// Resource is the resource descriptor to add.
	// The access specification will be updated with the new imageReference.
	Resource *v2.Resource `json:"resource"`
	// File is the access specification to the file containing the OCI artifact
	File v1alpha1.File `json:"file"`
	// TargetReference is the target OCI image reference where the artifact should be uploaded
	TargetReference string `json:"targetReference"`
}
