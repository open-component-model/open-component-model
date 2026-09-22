package v1

import (
	"errors"

	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	Type = "git"
)

// Git describes a repository snapshot archived during component construction
// and stored as a local blob in the component version.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Git struct {
	// +ocm:jsonschema-gen:enum=git/v1
	// +ocm:jsonschema-gen:enum:deprecated=git
	// +ocm:jsonschema-gen:enum:deprecated=Git
	// +ocm:jsonschema-gen:enum:deprecated=Git/v1
	Type runtime.Type `json:"type"`

	// Repository is the Git repository URL.
	Repository string `json:"repository"`

	// Ref selects a Git ref. If both Ref and Commit are empty, remote HEAD is used.
	Ref string `json:"ref,omitempty"`

	// Commit pins a commit by its full 40-character hexadecimal SHA and takes
	// precedence over Ref.
	Commit string `json:"commit,omitempty"`
}

func (g *Git) String() string {
	return g.Repository
}

func (g *Git) Validate() error {
	if g == nil {
		return errors.New("git input is required")
	}

	// Inputs may follow remote HEAD, unlike access specs which require a selector.
	ref := g.Ref
	if ref == "" && g.Commit == "" {
		ref = "HEAD"
	}
	return (&accessv1.Git{Repository: g.Repository, Ref: ref, Commit: g.Commit}).Validate()
}
