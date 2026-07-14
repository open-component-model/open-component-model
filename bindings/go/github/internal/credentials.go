package internal

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/github/spec/access"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// CredentialConsumerIdentity resolves the credential consumer identity for the
// given GitHub repository URL. The identity type is GitHubRepository and works
// for github.com as well as GitHub Enterprise hosts.
func CredentialConsumerIdentity(repoURL string) (runtime.Identity, error) {
	if repoURL == "" {
		return nil, fmt.Errorf("no github repository specified")
	}

	identity, err := runtime.ParseURLToIdentity(repoURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing github repository URL to identity: %w", err)
	}
	identity.SetType(runtime.NewUnversionedType(access.GitHubRepositoryConsumerType))

	return identity, nil
}
