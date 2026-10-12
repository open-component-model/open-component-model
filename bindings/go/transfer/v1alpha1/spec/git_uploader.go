package spec

import (
	"fmt"
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
// Without match it uses [DefaultGitUploaderMatch] (Git accesses). Repository and ref are
// required and must be set explicitly: repository is the target Git repository URL, ref the
// full branch or tag ref the commit is pushed to. A Git access must be pinned to a commit:
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	  - type: git.uploader.transfer.config.ocm.software/v1alpha1
//	    repository: https://git.example.com/org/repo.git
//	    ref: refs/heads/main
//
// A Git resource copied by value (e.g. across an air gap) becomes a local blob that no
// longer carries its origin. The default match does not select it; push it with an explicit
// match (on [GitLocalBlobMediaType]), repository and ref.
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
	// `component` and `target`) or a plain literal. Required.
	Repository string `json:"repository"`

	// Ref is the full branch or tag ref the commit is pushed to, e.g. refs/heads/main or
	// refs/tags/v1.0.0: a CEL expression wrapped in ${...} or a plain literal. Required; a
	// short name is rejected because it does not say whether it is a branch or a tag.
	Ref string `json:"ref"`
}

// EffectiveMatch returns the configured match, or [DefaultGitUploaderMatch]. It implements
// [UploaderConfig].
func (u *GitUploaderConfig) EffectiveMatch() string {
	return matchOrDefault(u.Match, DefaultGitUploaderMatch)
}

// Validate rejects a non-matching Type and a config missing repository or ref, which must
// both be set explicitly. An empty Type is allowed for programmatically constructed configs.
func (u *GitUploaderConfig) Validate() error {
	if u == nil {
		return nil
	}
	if err := validateUploaderType(u.Type, GitUploaderConfigType); err != nil {
		return err
	}
	if strings.TrimSpace(u.Repository) == "" {
		return fmt.Errorf("repository is required")
	}
	if strings.TrimSpace(u.Ref) == "" {
		return fmt.Errorf("ref is required")
	}
	return nil
}
