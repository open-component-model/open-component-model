package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/repository/resource"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const GetOCIArtifactType = "GetOCIArtifact"

// GetOCIArtifact is a transformer specification to get an OCI artifact
// from a remote OCI registry and buffer it to a file.
// It contains the resource descriptor of the artifact to be retrieved and an optional output path.
// If no output path is given, a temporary file will be created for buffering the artifact.
// Spec: GetOCIArtifactSpec - the input specification of the transformation containing the resource descriptor and output path.
// Output: GetOCIArtifactOutput - the output specification of the transformation containing the file access specification
// for the downloaded artifact and the resource descriptor from the component.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type GetOCIArtifact struct {
	// +ocm:jsonschema-gen:enum=GetOCIArtifact/v1alpha1
	Type   runtime.Type          `json:"type"`
	ID     string                `json:"id"`
	Spec   *GetOCIArtifactSpec   `json:"spec"`
	Output *GetOCIArtifactOutput `json:"output,omitempty"`
}

// GetOCIArtifactOutput is the output specification of the
// GetOCIArtifact transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type GetOCIArtifactOutput struct {
	// File is the file access specification for the downloaded artifact
	File v1alpha1.File `json:"file"`
	// Resource is the resource descriptor from the component
	Resource *v2.Resource `json:"resource"`
}

// GetOCIArtifactSpec is the input specification for the
// GetOCIArtifact transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type GetOCIArtifactSpec struct {
	// Repository to download the OCI artifact from.
	Repository *resource.ResourceRepository `json:"repository"`
	// Resource is the resource descriptor to get the OCI artifact from.
	Resource *v2.Resource `json:"resource"`
	// OutputPath is the path where the artifact should be downloaded to.
	// If empty, a temporary file will be created.
	OutputPath string `json:"outputPath,omitempty"`
}
