package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

var GetGitHubCommitV1alpha1 = runtime.NewVersionedType(GetGitHubCommitType, Version)

func init() {
	Scheme.MustRegisterWithAlias(&GetGitHubCommit{}, GetGitHubCommitV1alpha1)
}
