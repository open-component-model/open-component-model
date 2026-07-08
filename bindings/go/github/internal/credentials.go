package internal

import (
	"fmt"

	credv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/github/spec/access"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	credentialKeyToken       = "token"
	credentialKeyAccessToken = "accessToken"
)

var credentialScheme = runtime.NewScheme()

func init() {
	credv1.MustRegister(credentialScheme)
}

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

// TokenFromCredentials extracts a GitHub access token from OCM credentials.
// A nil input or an empty type means anonymous access (empty token, no error).
// Credentials that are present but carry no usable token are rejected rather
// than silently downgraded to an unauthenticated request.
func TokenFromCredentials(credentials runtime.Typed) (string, error) {
	if credentials == nil || credentials.GetType().String() == "" {
		return "", nil
	}

	typed, err := credentialScheme.NewObject(credentials.GetType())
	if err != nil {
		return "", fmt.Errorf("error converting credential type: %w", err)
	}
	if err := credentialScheme.Convert(credentials, typed); err != nil {
		return "", fmt.Errorf("error converting credential type: %w", err)
	}
	direct, ok := typed.(*credv1.DirectCredentials)
	if !ok {
		return "", fmt.Errorf("unsupported credential type for github access: %v", credentials.GetType())
	}

	if token := direct.Properties[credentialKeyToken]; token != "" {
		return token, nil
	}
	if token := direct.Properties[credentialKeyAccessToken]; token != "" {
		return token, nil
	}
	return "", fmt.Errorf("credentials were provided but contain no github token; refusing to fall back to anonymous access")
}
