package v1

import (
	"fmt"
	"net/url"
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
	// host and a path, or the scp-like form user@host:path. A fragment may select the ref
	// or commit with branch=<name>, tag=<name> or commit=<sha>, as in
	// https://github.com/org/repo.git#branch=main.
	// Local repositories (file:// or plain paths) are not supported for declared git
	// accesses; use a git input instead, which archives a local repository into a
	// portable local blob at component version creation time.
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

// ValidateStructure checks that the access fields are structurally valid: the
// repository parses as a known endpoint, the ref name is legal and at least one
// of ref or commit is set. It is permissive about local repositories (file:// or
// plain paths), because the fetch engine (used by git inputs) needs to clone them.
func (g *Git) ValidateStructure() error {
	g, err := g.Normalize()
	if err != nil {
		return err
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

// Validate checks that the access is structurally valid and that the repository
// is not a local path or file:// URL. Local repositories are not supported for
// declared git accesses because no remote consumer can reach them; use a git
// input instead, which archives the local repository into a portable local blob.
func (g *Git) Validate() error {
	if err := g.ValidateStructure(); err != nil {
		return err
	}
	normalized, err := g.Normalize()
	if err != nil {
		return err
	}
	ep, err := endpoint.Parse(normalized.Repository)
	if err != nil {
		return err
	}
	if ep.IsLocal() {
		return endpoint.ErrLocalRepository
	}
	return nil
}

// Normalize returns a copy of g with the ref or commit selected by the repository URL
// fragment moved into Ref and Commit and the fragment removed, so that readers without
// fragment support, such as OCM v1, find the same revision. A fragment selects with
// branch=<name>, tag=<name> or commit=<sha>, as in https://host/org/repo.git#branch=main.
// A fragment selector and a set field must not name different revisions.
func (g *Git) Normalize() (*Git, error) {
	if g == nil {
		return nil, fmt.Errorf("git access is required")
	}

	normalized := g.DeepCopy()
	repository, fragment, found := strings.Cut(g.Repository, "#")
	if !found {
		return normalized, nil
	}

	ref, commit, err := parseFragment(fragment)
	if err != nil {
		return nil, err
	}

	normalized.Repository = repository
	if normalized.Ref, err = mergeRef(g.Ref, ref); err != nil {
		return nil, err
	}
	if normalized.Commit, err = mergeCommit(g.Commit, commit); err != nil {
		return nil, err
	}

	return normalized, nil
}

func (g *Git) NormalizeTyped() (runtime.Typed, error) {
	return g.Normalize()
}

// parseFragment decodes with url.PathUnescape rather than url.ParseQuery, which
// reads + as a space and would break tags such as v1.2.3+build.1.
func parseFragment(fragment string) (string, string, error) {
	var ref, commit string
	seen := map[string]bool{}
	for pair := range strings.SplitSeq(fragment, "&") {
		if pair == "" {
			continue
		}
		key, value, _ := strings.Cut(pair, "=")
		value, err := url.PathUnescape(value)
		if err != nil {
			return "", "", fmt.Errorf("invalid git repository URL fragment")
		}
		if value == "" || seen[key] {
			return "", "", fmt.Errorf("git repository URL fragment %q requires exactly one value", key)
		}
		seen[key] = true

		switch key {
		case "branch":
			if strings.HasPrefix(value, "refs/") {
				if !plumbing.ReferenceName(value).IsBranch() {
					return "", "", fmt.Errorf("branch fragment requires a branch ref")
				}
				ref = value
			} else {
				ref = plumbing.NewBranchReferenceName(value).String()
			}
		case "tag":
			if strings.HasPrefix(value, "refs/") {
				if !plumbing.ReferenceName(value).IsTag() {
					return "", "", fmt.Errorf("tag fragment requires a tag ref")
				}
				ref = value
			} else {
				ref = plumbing.NewTagReferenceName(value).String()
			}
		case "commit":
			commit = value
		default:
			return "", "", fmt.Errorf("unsupported git repository URL fragment %q, expected branch, tag or commit", key)
		}
	}

	if seen["branch"] && seen["tag"] {
		return "", "", fmt.Errorf("git repository URL fragment must not set both branch and tag")
	}

	return ref, commit, nil
}

// mergeRef accepts a ref that names the selected ref fully or as a short branch or tag name.
func mergeRef(ref, selected string) (string, error) {
	if selected == "" {
		return ref, nil
	}
	if ref != "" && ref != selected && plumbing.NewBranchReferenceName(ref).String() != selected &&
		plumbing.NewTagReferenceName(ref).String() != selected {
		return "", fmt.Errorf("git ref %q conflicts with %q selected by the repository URL", ref, selected)
	}
	return selected, nil
}

func mergeCommit(commit, selected string) (string, error) {
	if selected == "" {
		return commit, nil
	}
	if commit != "" && !strings.EqualFold(commit, selected) {
		return "", fmt.Errorf("git commit %q conflicts with %q selected by the repository URL", commit, selected)
	}
	return selected, nil
}
