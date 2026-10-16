package v1

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"

	"ocm.software/open-component-model/bindings/go/git/internal/endpoint"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Git identifies a Git repository snapshot. Commit takes precedence over Ref.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Git struct {
	// +ocm:jsonschema-gen:enum=Git/v1,Git
	// +ocm:jsonschema-gen:enum:deprecated=git,git/v1alpha1,Git/v1alpha1
	Type runtime.Type `json:"type"`

	// Repository is the Git repository URL: an http(s)://, ssh:// or git:// URL with a
	// host and a path, the scp-like form user@host:path, or a local repository as
	// file:///path or a plain path. A value without a scheme that is not scp-like is a
	// local path, relative to the working directory unless absolute. A fragment may
	// select the ref or commit with branch=<name>, tag=<name> or commit=<sha>, as in
	// https://github.com/org/repo.git#branch=main.
	Repository string `json:"repository"`

	// Ref selects a branch, tag or full ref name. A bare name resolves to a
	// matching branch first and then to a tag, so use refs/tags/v1.0.0 to select
	// that tag unambiguously. Digest processing resolves it to a commit and pins the
	// commit; the ref is recorded as written, not expanded to its full name.
	Ref string `json:"ref,omitempty"`

	// Commit pins a commit by its full 40-character hexadecimal SHA and takes
	// precedence over Ref. At least one of Commit or Ref must be set.
	Commit string `json:"commit,omitempty"`
}

var commitSHA = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// Validate checks a normalized access. Decoding normalizes; an access built in code
// with a fragment in Repository must be normalized first, so that no reader has to
// look for selectors in the URL.
func (g *Git) Validate() error {
	if g == nil {
		return fmt.Errorf("git access is required")
	}
	if strings.Contains(g.Repository, "#") {
		return fmt.Errorf("git repository URL fragment must be moved into ref or commit, see Normalize")
	}
	if _, err := endpoint.Parse(g.Repository); err != nil {
		return err
	}

	if g.Ref == "" && g.Commit == "" {
		return fmt.Errorf("either commit or ref must be set")
	}

	if g.Commit != "" && !commitSHA.MatchString(g.Commit) {
		return fmt.Errorf("commit must be a 40-character hexadecimal SHA")
	}

	if g.Ref != "" && g.Ref != "HEAD" {
		ref := g.Ref
		if !strings.HasPrefix(ref, "refs/") {
			ref = "refs/heads/" + ref
		}

		if err := plumbing.ReferenceName(ref).Validate(); err != nil {
			return fmt.Errorf("invalid git ref: %w", err)
		}
	}

	return nil
}

// UnmarshalJSON normalizes the decoded access, see Normalize.
func (g *Git) UnmarshalJSON(data []byte) error {
	type plain Git
	if err := json.Unmarshal(data, (*plain)(g)); err != nil {
		return err
	}
	return g.moveFragment()
}

// Normalize returns a copy of g with the ref or commit selected by the repository URL
// fragment moved into Ref and Commit and the fragment removed, so that readers without
// fragment support, such as OCM v1, find the same revision. A fragment selector and a
// set field must not name different revisions. Decoding normalizes already; only an
// access built in code needs this.
func (g *Git) Normalize() (*Git, error) {
	if g == nil {
		return nil, fmt.Errorf("git access is required")
	}

	normalized := g.DeepCopy()
	if err := normalized.moveFragment(); err != nil {
		return nil, err
	}

	return normalized, nil
}

func (g *Git) moveFragment() error {
	repository, fragment, found := strings.Cut(g.Repository, "#")
	if !found {
		return nil
	}

	selected, err := endpoint.ParseFragment(fragment)
	if err != nil {
		return err
	}

	ref, commit, err := selected.Selectors(g.Ref, g.Commit)
	if err != nil {
		return err
	}

	g.Repository, g.Ref, g.Commit = repository, ref, commit
	return nil
}
