package v1

import (
	"fmt"
	"regexp"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// Type is the canonical access type, using the GitHub brand casing.
	Type = "GitHub"
	// LegacyType is the lowercase alias registered by old OCM
	// (github.com/open-component-model/ocm). Kept so component descriptors
	// written with type "github"/"github/v1" keep resolving.
	LegacyType = "github"
	// CamelLegacyType is the camelCase spelling used by the OCM spec
	// (https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/02-access-types/github.md).
	// Kept as a deprecated alias so descriptors written with type
	// "gitHub"/"gitHub/v1" keep resolving.
	CamelLegacyType = "gitHub"
)

// GitHub describes the access to a commit of a GitHub repository, downloadable
// as an archive via the GitHub REST API.
//
// The canonical type is "GitHub" (brand casing). The lowercase "github" and
// camelCase "gitHub" spellings used by old OCM
// (https://github.com/open-component-model/ocm/blob/main/api/ocm/extensions/accessmethods/github/method.go)
// and the OCM spec are retained as deprecated aliases so existing component
// descriptors keep resolving.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type GitHub struct {
	// +ocm:jsonschema-gen:enum=GitHub/v1,GitHub
	// +ocm:jsonschema-gen:enum:deprecated=github/v1,github,gitHub/v1,gitHub
	Type runtime.Type `json:"type"`

	// RepoURL is the full repository URL (e.g. https://github.com/open-component-model/ocm).
	RepoURL string `json:"repoUrl"`

	// APIHostname overrides the GitHub REST API hostname for GitHub Enterprise.
	APIHostname string `json:"apiHostname,omitempty"`

	// Commit is the 40-hex-character SHA of the commit to access.
	//
	// This access type is used for both resources and sources. A source requires
	// a commit. A resource may be authored with only a Ref and have its Commit
	// pinned later, which is why the field is omitempty. Validate() rejects a
	// spec that sets neither Commit nor Ref; a set Commit takes precedence over
	// Ref.
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
