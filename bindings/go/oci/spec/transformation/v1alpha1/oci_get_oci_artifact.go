package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const OCIGetOCIArtifactType = "OCIGetOCIArtifact"

// OCIGetOCIArtifact is a transformer specification to get an OCI artifact
// from a remote OCI registry and buffer it to a file.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type OCIGetOCIArtifact struct {
	// +ocm:jsonschema-gen:enum=OCIGetOCIArtifact/v1alpha1
	Type   runtime.Type             `json:"type"`
	ID     string                   `json:"id"`
	Spec   *OCIGetOCIArtifactSpec   `json:"spec"`
	Output *OCIGetOCIArtifactOutput `json:"output,omitempty"`
}

// OCIGetOCIArtifactOutput is the output specification of the
// OCIGetOCIArtifact transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type OCIGetOCIArtifactOutput struct {
	// File is the file access specification for the downloaded artifact
	File v1alpha1.File `json:"file"`
	// Resource is the resource descriptor from the component
	Resource *v2.Resource `json:"resource"`
}

// OCIGetOCIArtifactSpec is the input specification for the
// OCIGetOCIArtifact transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type OCIGetOCIArtifactSpec struct {
	// Resource identifies the oci artifact to retrieve.
	Resource *v2.Resource `json:"resource"`
	// OutputPath is the path where the artifact should be buffered.
	// If empty, a temporary file will be created.
	OutputPath string `json:"outputPath,omitempty"`

	// TODO referenceName must be set in transformer
}
