package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const AddGitResourceType = "AddGitResource"

// AddGitResource pushes the history of a Git archive, as GetGitResource buffers it, into
// the existing Git repository and ref named by the Git access of its resource.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type AddGitResource struct {
	// +ocm:jsonschema-gen:enum=AddGitResource/v1alpha1
	Type   runtime.Type          `json:"type"`
	ID     string                `json:"id"`
	Spec   *AddGitResourceSpec   `json:"spec"`
	Output *AddGitResourceOutput `json:"output,omitempty"`
}

// AddGitResourceSpec names the target and the archive to push.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type AddGitResourceSpec struct {
	// Resource is the resource to publish. Its Git access names the target repository
	// and the branch or tag, by full ref or bare name; a set commit must be the archived
	// commit. Its digest, if set, is verified against the archive.
	Resource *v2.Resource `json:"resource"`
	// File is the Git archive to push.
	File v1alpha1.File `json:"file"`
}

// AddGitResourceOutput carries the published resource, with Git access to the target
// repository, ref and the pushed commit.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type AddGitResourceOutput struct {
	Resource *v2.Resource `json:"resource"`
}
