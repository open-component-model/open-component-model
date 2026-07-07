package access

import (
	"ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// GitHubRepositoryConsumerType is the credential consumer type for GitHub
// repositories. Credentials for this consumer typically carry a "token"
// property holding a GitHub (Enterprise) access token.
const GitHubRepositoryConsumerType = "GitHubRepository"

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	gitHub := &v1.GitHub{}
	scheme.MustRegisterWithAlias(gitHub,
		runtime.NewVersionedType(v1.Type, v1.Version),
		runtime.NewUnversionedType(v1.Type),
		runtime.NewVersionedType(v1.LegacyType, v1.Version),
		runtime.NewUnversionedType(v1.LegacyType),
	)
}
