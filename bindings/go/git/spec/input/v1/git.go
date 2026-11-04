package v1

import (
	"errors"

	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Git describes a repository snapshot archived during component construction
// and stored as a local blob in the component version.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Git struct {
	// +ocm:jsonschema-gen:enum=Git/v1,Git
	// +ocm:jsonschema-gen:enum:deprecated=git,git/v1
	Type runtime.Type `json:"type"`

	// Repository is the Git repository URL: an http(s)://, ssh:// or git:// URL with a
	// host and a path, the scp-like form user@host:path, or a local repository as
	// file:///path or a plain path. A value without a scheme that is not scp-like is a
	// local path, relative to the working directory unless absolute. A fragment may
	// select the ref or commit with branch=<name>, tag=<name> or commit=<sha>, as in
	// https://github.com/org/repo.git#branch=main.
	Repository string `json:"repository"`

	// Ref selects a branch, tag or full ref name (for example main, v1.0.0 or
	// refs/tags/v1.0.0). If both Ref and Commit are empty, remote HEAD is used.
	Ref string `json:"ref,omitempty"`

	// Commit pins a commit by its full 40-character hexadecimal SHA and takes
	// precedence over Ref.
	Commit string `json:"commit,omitempty"`
}

func (g *Git) String() string {
	return g.Repository
}

func (g *Git) Validate() error {
	access, err := g.ToAccess()
	if err != nil {
		return err
	}

	return access.Validate()
}

// ToAccess returns the normalized Git access that selects the input snapshot.
func (g *Git) ToAccess() (*accessv1.Git, error) {
	if g == nil {
		return nil, errors.New("git input is required")
	}

	access, err := (&accessv1.Git{Repository: g.Repository, Ref: g.Ref, Commit: g.Commit}).Normalize()
	if err != nil {
		return nil, err
	}

	// Inputs may follow remote HEAD, unlike access specs which require a selector.
	if access.Ref == "" && access.Commit == "" {
		access.Ref = "HEAD"
	}

	return access, nil
}
