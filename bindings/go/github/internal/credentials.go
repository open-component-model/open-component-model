package internal

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/github/spec/access"
	credsv1 "ocm.software/open-component-model/bindings/go/github/spec/credentials/v1"
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

// CredentialsFrom converts OCM credentials into the typed GitHub credentials
// the download path authenticates with. Absent credentials mean anonymous
// access and yield nil without an error.
//
// Credentials that are present but carry no token are rejected rather than
// downgraded to an anonymous request: GitHub answers an unauthenticated read of
// a private repository with 404 rather than 403, so a misconfigured secret would
// otherwise surface as "repository does not exist".
func CredentialsFrom(credentials runtime.Typed) (*credsv1.GitHubCredentials, error) {
	gitHubCredentials, err := credsv1.ConvertToGitHubCredentials(credentials)
	if err != nil {
		return nil, err
	}
	if gitHubCredentials == nil {
		return nil, nil
	}
	if gitHubCredentials.Token == "" {
		return nil, fmt.Errorf("credentials were provided but contain no github token; refusing to fall back to anonymous access")
	}

	return gitHubCredentials, nil
}
