package v1

import (
	"fmt"

	directcredsv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	credentialKeyUsername      = "username"
	credentialKeyPassword      = "password"
	credentialKeyIdentityToken = "identityToken"
	// credentialKeyAccessToken is the token key old OCM read for repositories.
	// Property bags written for it must keep working.
	credentialKeyAccessToken = "accessToken"
)

var convertScheme = runtime.NewScheme()

func init() {
	// Register the same spellings as the public Scheme, so the two cannot
	// disagree about which types resolve.
	MustRegisterCredentialType(convertScheme)
	// The credential graph resolves .ocmconfig entries into DirectCredentials
	// property bags, so the converter must be able to decode that type too.
	directcredsv1.MustRegister(convertScheme)
}

// ConvertToPyPICredentials converts runtime.Typed credentials into
// *PyPICredentials, accepting either a typed credential or a
// DirectCredentials/v1 property bag.
//
// Nil, an empty type, or a property bag without any PyPI key yields nil without
// an error: most PyPI indexes are readable anonymously, so absent credentials
// are not a failure. Rejecting credentials that are present but unusable is the
// caller's job.
func ConvertToPyPICredentials(creds runtime.Typed) (*PyPICredentials, error) {
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
	case *PyPICredentials:
		return t, nil
	}

	return nil, fmt.Errorf("unsupported credential type for pypi access: %v", typed.GetType())
}

// fromDirectCredentials decodes the property bag legacy .ocmconfig files carry.
// "identityToken" is the typed spelling; "accessToken" is old OCM's, and is
// used only when the typed key is absent. A bag that holds none of the PyPI
// keys yields nil: a consumer entry for the host that carries, say, only a
// certificate must not turn an anonymous download into a credential error.
func fromDirectCredentials(properties map[string]string) *PyPICredentials {
	token := properties[credentialKeyIdentityToken]
	if token == "" {
		token = properties[credentialKeyAccessToken]
	}
	username, password := properties[credentialKeyUsername], properties[credentialKeyPassword]
	if token == "" && username == "" && password == "" {
		return nil
	}
	return &PyPICredentials{
		Type:          runtime.NewVersionedType(PyPICredentialsType, Version),
		Username:      username,
		Password:      password,
		IdentityToken: token,
	}
}
