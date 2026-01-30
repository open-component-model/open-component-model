package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const CTFGetOCIArtifactType = "CTFGetOCIArtifact"

// CTFGetOCIArtifact is a transformer specification to get an OCI artifact
// from a remote OCI registry (referenced in a CTF component) and buffer it to a file.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type CTFGetOCIArtifact struct {
	// +ocm:jsonschema-gen:enum=CTFGetOCIArtifact/v1alpha1
	Type   runtime.Type             `json:"type"`
	ID     string                   `json:"id"`
	Spec   *CTFGetOCIArtifactSpec   `json:"spec"`
	Output *CTFGetOCIArtifactOutput `json:"output,omitempty"`
}

// CTFGetOCIArtifactOutput is the output specification of the
// CTFGetOCIArtifact transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type CTFGetOCIArtifactOutput struct {
	// File is the file access specification for the downloaded artifact
	File v1alpha1.File `json:"file"`
	// Resource is the resource descriptor from the component
	Resource *v2.Resource `json:"resource"`
}

// CTFGetOCIArtifactSpec is the input specification for the
// CTFGetOCIArtifact transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type CTFGetOCIArtifactSpec struct {
	// Repository is the CTF repository specification
	Repository ctf.Repository `json:"repository"`
	// Component is the component name
	Component string `json:"component"`
	// Version is the component version
	Version string `json:"version"`
	// ResourceIdentity identifies the resource to retrieve.
	// Must match a resource in the component descriptor.
	ResourceIdentity runtime.Identity `json:"resourceIdentity"`
	// OutputPath is the path where the artifact should be buffered.
	// If empty, a temporary file will be created.
	OutputPath string `json:"outputPath,omitempty"`
}
