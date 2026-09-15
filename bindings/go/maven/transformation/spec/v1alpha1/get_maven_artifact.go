package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const GetMavenArtifactType = "GetMavenArtifact"

// GetMavenArtifact is a transformer specification to get the files of a Maven
// artifact. It specifies the resource carrying the maven/v2alpha1 access and
// the output path where the download archive should be buffered to. The
// archive is the application/x-tgz the Maven resource repository produces:
// every file listed in the access spec, each followed by the sibling
// signature and checksum files the repository has for it.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type GetMavenArtifact struct {
	// +ocm:jsonschema-gen:enum=GetMavenArtifact/v1alpha1
	Type   runtime.Type            `json:"type"`
	ID     string                  `json:"id"`
	Spec   *GetMavenArtifactSpec   `json:"spec"`
	Output *GetMavenArtifactOutput `json:"output,omitempty"`
}

// GetMavenArtifactSpec is the input specification for the GetMavenArtifact
// transformation. Optionally, an output path can be specified where the
// archive should be buffered to. If not specified, a temporary file will be
// created.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type GetMavenArtifactSpec struct {
	// Resource is the resource descriptor to get the artifact from.
	Resource *v2.Resource `json:"resource"`
	// OutputPath is the directory the archive should be buffered into.
	// If empty, a temporary file will be created.
	OutputPath string `json:"outputPath,omitempty"`
}

// GetMavenArtifactOutput is the output specification of the GetMavenArtifact
// transformation. It contains the file access specification for the buffered
// archive as well as the resource descriptor.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type GetMavenArtifactOutput struct {
	// File is the file access specification for the gzipped tar archive
	// holding the artifact files and their sibling signature and checksum files.
	File v1alpha1.File `json:"file"`
	// Resource is the resource descriptor from the component.
	Resource *v2.Resource `json:"resource"`
}
