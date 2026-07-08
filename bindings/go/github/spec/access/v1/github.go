package v1

import (
	"fmt"
	"regexp"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// GitHub describes the access to a commit of a GitHub repository, downloadable
// as an archive via the GitHub REST API.
// This spec is aligned with ocm v1 https://github.com/open-component-model/ocm/blob/main/api/ocm/extensions/accessmethods/github/method.go
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type GitHub struct {
	// +ocm:jsonschema-gen:enum=github/v1,GitHub/v1
	// +ocm:jsonschema-gen:enum:deprecated=gitHub,github,GitHub
	Type runtime.Type `json:"type"`

	// RepoURL is the full repository URL (e.g. https://github.com/open-component-model/ocm).
	RepoURL string `json:"repoUrl"`

	// APIHostname overrides the GitHub REST API hostname for GitHub Enterprise.
	APIHostname string `json:"apiHostname,omitempty"`

	// Commit is the 40-hex-character SHA of the commit to access.
	Commit string `json:"commit,omitempty"`

	// Ref is a git reference (e.g. refs/heads/main). A resource may be authored
	// with only a Ref and have its Commit pinned later by the constructor's
	// digest processor; once a Commit is present it is authoritative and Ref is
	// informational.
	Ref string `json:"ref,omitempty"`
}

// commitSHARegex matches a full 40-character hexadecimal git commit SHA.
var commitSHARegex = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// Validate checks that the access spec is well-formed: it has a repository URL,
// carries at least one of Commit or Ref, and — when Commit is set — a full
// 40-hex SHA. A spec may be pinned to a Commit or carry only a Ref to be
// resolved and pinned later; once present, Commit is authoritative.
func (g *GitHub) Validate() error {
	if g.RepoURL == "" {
		return fmt.Errorf("repoUrl must not be empty")
	}
	if g.Commit == "" && g.Ref == "" {
		return fmt.Errorf("either commit or ref must be set")
	}
	if g.Commit != "" && !commitSHARegex.MatchString(g.Commit) {
		return fmt.Errorf("commit %q must be a 40-character hexadecimal SHA", g.Commit)
	}
	return nil
}
