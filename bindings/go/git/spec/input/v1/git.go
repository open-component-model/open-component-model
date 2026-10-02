package v1

import (
	"errors"

	"ocm.software/open-component-model/bindings/go/git/internal/endpoint"
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

	// Repository is the Git repository URL. A fragment may select the ref or
	// commit with branch=<name>, tag=<name> or commit=<sha>, as in
	// https://github.com/org/repo.git#branch=main.
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
	access, err := g.ToAccess()
	if err != nil {
		return err
	}

	return access.Validate()
}

// ToAccess returns the Git access that selects the input snapshot. A ref or
// commit selected by the repository URL fragment is moved into Ref or Commit.
func (g *Git) ToAccess() (*accessv1.Git, error) {
	if g == nil {
		return nil, errors.New("git input is required")
	}

	ep, err := endpoint.Parse(g.Repository)
	if err != nil {
		return nil, err
	}

	ref, commit, err := ep.Selectors(g.Ref, g.Commit)
	if err != nil {
		return nil, err
	}

	// Inputs may follow remote HEAD, unlike access specs which require a selector.
	if ref == "" && commit == "" {
		ref = "HEAD"
	}

	return &accessv1.Git{Repository: ep.Repository, Ref: ref, Commit: commit}, nil
}
