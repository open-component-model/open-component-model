package v1

import (
	"fmt"

	directcredsv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	credentialKeyToken       = "token"
	credentialKeyAccessToken = "accessToken"
)

var convertScheme = runtime.NewScheme()

func init() {
	// Register the same spellings as the public Scheme, so the two cannot
	// disagree about which types resolve.
	convertScheme.MustRegisterWithAlias(&GitHubCredentials{},
		runtime.NewVersionedType(GitHubCredentialsType, Version),
		runtime.NewUnversionedType(GitHubCredentialsType),
	)
	// The credential graph resolves .ocmconfig entries into DirectCredentials
	// property bags, so the converter must be able to decode that type too.
	directcredsv1.MustRegister(convertScheme)
}

// fromDirectCredentials maps a property bag, as legacy .ocmconfig files carry,
// onto typed credentials. "token" is old OCM's spelling and "accessToken" the
// one shared with the HTTP and OCI access types; "token" wins when both are set.
func fromDirectCredentials(properties map[string]string) *GitHubCredentials {
	token := properties[credentialKeyToken]
	if token == "" {
		token = properties[credentialKeyAccessToken]
	}
	return &GitHubCredentials{
		Type:  runtime.NewVersionedType(GitHubCredentialsType, Version),
		Token: token,
	}
}

// ConvertToGitHubCredentials converts runtime.Typed credentials into
// *GitHubCredentials, accepting either a typed credential or a
// DirectCredentials/v1 property bag.
//
// Nil, or an empty type, yields nil without an error: the github access is
// usable anonymously, so absent credentials are not a failure. Rejecting
// credentials that are present but unusable is the caller's job.
func ConvertToGitHubCredentials(creds runtime.Typed) (*GitHubCredentials, error) {
	if creds == nil || creds.GetType().String() == "" {
		return nil, nil
	}

	typed, err := convertScheme.NewObject(creds.GetType())
	if err != nil {
		return nil, fmt.Errorf("error creating credential object for type %q: %w", creds.GetType(), err)
	}
	if err := convertScheme.Convert(creds, typed); err != nil {
		return nil, fmt.Errorf("error converting credentials of type %q: %w", creds.GetType(), err)
	}

	switch t := typed.(type) {
	case *directcredsv1.DirectCredentials:
		return fromDirectCredentials(t.Properties), nil
	case *GitHubCredentials:
		return t, nil
	}

	return nil, fmt.Errorf("unsupported credential type for github access: %v", typed.GetType())
}
