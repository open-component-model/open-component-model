package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const GetGitResourceType = "GetGitResource"

// GetGitResource downloads the repository tree selected by a resource's Git access
// as a gzipped tar archive. A commit takes precedence over a ref, as in Git access.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type GetGitResource struct {
	// +ocm:jsonschema-gen:enum=GetGitResource/v1alpha1
	Type   runtime.Type          `json:"type"`
	ID     string                `json:"id"`
	Spec   *GetGitResourceSpec   `json:"spec"`
	Output *GetGitResourceOutput `json:"output,omitempty"`
}

// GetGitResourceSpec configures the resource download and archive destination.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type GetGitResourceSpec struct {
	Resource *v2.Resource `json:"resource"`
	// OutputPath is an existing directory in which to create the archive file.
	// If empty, the operating system's temporary directory is used.
	OutputPath string `json:"outputPath,omitempty"`
}

// GetGitResourceOutput carries the buffered archive and the original resource
// descriptor, including its digest, for subsequent transfer transformations.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type GetGitResourceOutput struct {
	File     v1alpha1.File `json:"file"`
	Resource *v2.Resource  `json:"resource"`
}
