package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const GetGitHubCommitType = "GetGitHubCommit"

// GetGitHubCommit is a transformer specification to get the content of a
// GitHub repository at a pinned commit. It specifies the resource carrying
// the gitHub access and the output path where the repository tree should be
// buffered to as a tar archive.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type GetGitHubCommit struct {
	// +ocm:jsonschema-gen:enum=GetGitHubCommit/v1alpha1
	Type   runtime.Type           `json:"type"`
	ID     string                 `json:"id"`
	Spec   *GetGitHubCommitSpec   `json:"spec"`
	Output *GetGitHubCommitOutput `json:"output,omitempty"`
}

// GetGitHubCommitSpec is the input specification for the GetGitHubCommit
// transformation. Optionally, an output path can be specified where the
// archive should be buffered to. If not specified, a temporary file will be
// created.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type GetGitHubCommitSpec struct {
	// Resource is the resource descriptor to get the artifact from.
	Resource *v2.Resource `json:"resource"`
	// OutputPath is the path where the artifact should be downloaded to.
	// If empty, a temporary file will be created.
	OutputPath string `json:"outputPath,omitempty"`
}

// GetGitHubCommitOutput is the output specification of the GetGitHubCommit
// transformation. It contains the file access specification for the buffered
// tar archive of the repository tree as well as the resource descriptor.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type GetGitHubCommitOutput struct {
	// ContentFile is the file access specification for the tar archive
	// holding the repository tree at the pinned commit.
	ContentFile v1alpha1.File `json:"contentFile"`
	// Resource is the resource descriptor from the component.
	Resource *v2.Resource `json:"resource"`
}
