package internal

import (
	"fmt"
	"strings"

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

	// Default the scheme to https first: the access spec allows repoUrl with
	// or without a scheme, and without this the two spellings of the same
	// repository would yield different consumer identities.
	if !strings.HasPrefix(repoURL, "http://") && !strings.HasPrefix(repoURL, "https://") {
		repoURL = "https://" + repoURL
	}
	identity, err := runtime.ParseURLToIdentity(repoURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing github repository URL to identity: %w", err)
	}
	identity.SetType(runtime.NewUnversionedType(access.GitHubRepositoryConsumerType))

	return identity, nil
}
