package spec

import (
	"fmt"
	"strconv"
	"strings"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// GitUploaderConfigType is the config type that pushes matching Git resources, with
// their commit history, into an existing Git repository.
const GitUploaderConfigType = "git.uploader.transfer.config.ocm.software"

// GitLocalBlobMediaType marks a local blob that holds the archive of a Git resource copied
// by value, so it can be recognized as a Git archive (e.g. by an explicit git uploader
// match). The archive carries the commit history but not the origin; pushing such a local
// blob with the Git uploader needs an explicit repository and ref.
const GitLocalBlobMediaType = "application/vnd.ocm.software.git.archive.v1+tar+gzip"

// DefaultGitUploaderMatch is the match a [GitUploaderConfig] uses when none is set: Git
// accesses, regardless of the transfer target. A Git resource copied by value becomes a
// local blob that no longer carries its origin, so it is not selected by default; push it
// with an explicit match (on [GitLocalBlobMediaType]), repository and ref.
//
// Writing it explicitly into a config is equivalent to omitting match.
const DefaultGitUploaderMatch = `resource.access.isType("Git")`

// DefaultGitRef is the ref a [GitUploaderConfig] pushes to when Ref is empty: the ref of a
// Git access. It needs a Git access; a local blob needs an explicit ref. Writing it
// explicitly into a config is equivalent to omitting ref.
const DefaultGitRef = "${resource.access.toGit().ref}"

// DefaultGitRepository returns the repository a [GitUploaderConfig] pushes to when
// Repository is empty: the repository path of a Git access below baseURL. It needs a Git
// access; a local blob needs an explicit repository. Writing it explicitly into a config is
// equivalent to omitting repository.
func DefaultGitRepository(baseURL string) string {
	return "${" + strconv.Quote(strings.TrimSuffix(baseURL, "/")+"/") + " + resource.access.toGit().repository}"
}

func init() {
	Scheme.MustRegisterWithAlias(&GitUploaderConfig{},
		runtime.NewVersionedType(GitUploaderConfigType, Version),
		runtime.NewUnversionedType(GitUploaderConfigType),
	)
}

// GitUploaderConfig is a declarative rule that pushes the Git resources it selects into
// an existing Git repository during transfer. It is carried as an entry inside the
// central generic configuration (generic.config.ocm.software/v1), as a sibling of
// [Config], and extracted with [LookupUploaderConfigs].
//
// The resource is downloaded as the archive Git resources are stored as, which holds
// the complete history of the commit, and its commits are pushed unchanged: the commit
// SHA stays the same and no commit is created. Ref is pointed at the commit; a branch
// must fast-forward to it and a tag must not exist at another commit. The resource is
// published with a Git/v1 access on the repository, the full ref and the commit, which
// downloads to the same digest.
//
// Without match it uses [DefaultGitUploaderMatch] (Git accesses), without ref
// [DefaultGitRef], and without repository [DefaultGitRepository] below BaseURL. A Git
// access must be pinned to a commit:
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	  # push every Git resource below the same path on git.example.com, keeping its ref
//	  - type: git.uploader.transfer.config.ocm.software/v1alpha1
//	    baseUrl: https://git.example.com
//
// A Git resource copied by value (e.g. across an air gap) becomes a local blob that no
// longer carries its origin. The default match does not select it; push it with an explicit
// match (on [GitLocalBlobMediaType]), repository and ref, because BaseURL and the ref
// default need a Git access.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type GitUploaderConfig struct {
	// +ocm:jsonschema-gen:enum=git.uploader.transfer.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=git.uploader.transfer.config.ocm.software
	Type runtime.Type `json:"type"`

	// Match is a CEL boolean expression selecting the resources this uploader handles. It
	// sees `resource` and `target`; test access types with resource.access.isType. When empty,
	// DefaultGitUploaderMatch applies; an explicit value replaces it.
	Match string `json:"match,omitempty"`

	// Repository is the URL of the existing Git repository to push into, in any form a
	// Git/v1 access accepts: a CEL expression wrapped in ${...} (seeing `resource`,
	// `component` and `target`; resource.access.toGit() returns a Git access's repository
	// path and ref) or a plain literal. Exactly one of Repository and BaseURL is required.
	Repository string `json:"repository,omitempty"`

	// BaseURL is the URL below which the repository is addressed by the repository path of
	// a Git access's origin, e.g. https://git.example.com turns https://github.com/org/repo.git
	// into https://git.example.com/org/repo.git. It needs a Git access; a local blob needs an
	// explicit repository. Exactly one of Repository and BaseURL is required.
	BaseURL string `json:"baseUrl,omitempty"`

	// Ref is the full branch or tag ref the commit is pushed to, e.g. refs/heads/main or
	// refs/tags/v1.0.0: a CEL expression wrapped in ${...} or a plain literal. When empty,
	// DefaultGitRef applies, which needs a Git access; a local blob, or a Git access whose
	// recorded ref is a short name (pinned before digest processing recorded full refs),
	// needs an explicit Ref.
	Ref string `json:"ref,omitempty"`
}

// EffectiveMatch returns the configured match, or [DefaultGitUploaderMatch]. It implements
// [UploaderConfig].
func (u *GitUploaderConfig) EffectiveMatch() string {
	return matchOrDefault(u.Match, DefaultGitUploaderMatch)
}

// Validate rejects a non-matching Type and a config that sets neither or both of
// repository and baseUrl. An empty Type is allowed for programmatically constructed configs.
func (u *GitUploaderConfig) Validate() error {
	if u == nil {
		return nil
	}
	if err := validateUploaderType(u.Type, GitUploaderConfigType); err != nil {
		return err
	}
	hasRepository, hasBaseURL := strings.TrimSpace(u.Repository) != "", strings.TrimSpace(u.BaseURL) != ""
	if hasRepository == hasBaseURL {
		return fmt.Errorf("exactly one of repository and baseUrl is required")
	}
	return nil
}
