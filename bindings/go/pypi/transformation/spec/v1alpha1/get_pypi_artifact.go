package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const GetPyPIArtifactType = "GetPyPIArtifact"

// GetPyPIArtifact is a transformer specification to get the files of a PyPI
// distribution. It specifies the resource carrying the pypi/v1alpha1 access and
// the output path where the download archive should be buffered to. The
// archive is the application/x-tgz the PyPI resource repository produces:
// every distribution file the access spec selects, each followed by the
// detached signature the index has for it.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type GetPyPIArtifact struct {
	// +ocm:jsonschema-gen:enum=GetPyPIArtifact/v1alpha1
	Type   runtime.Type           `json:"type"`
	ID     string                 `json:"id"`
	Spec   *GetPyPIArtifactSpec   `json:"spec"`
	Output *GetPyPIArtifactOutput `json:"output,omitempty"`
}

// GetPyPIArtifactSpec is the input specification for the GetPyPIArtifact
// transformation. Optionally, an output path can be specified where the
// archive should be buffered to. If not specified, a temporary file will be
// created.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type GetPyPIArtifactSpec struct {
	// Resource is the resource descriptor to get the artifact from.
	Resource *v2.Resource `json:"resource"`
	// OutputPath is the directory the archive should be buffered into.
	// If empty, a temporary file will be created.
	OutputPath string `json:"outputPath,omitempty"`
}

// GetPyPIArtifactOutput is the output specification of the GetPyPIArtifact
// transformation. It contains the file access specification for the buffered
// archive as well as the resource descriptor.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type GetPyPIArtifactOutput struct {
	// File is the file access specification for the gzipped tar archive
	// holding the distribution files and their detached signatures.
	File v1alpha1.File `json:"file"`
	// Resource is the resource descriptor from the component.
	Resource *v2.Resource `json:"resource"`
}
