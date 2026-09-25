package v1

import (
	"fmt"

	directcredsv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// The property names are the ones OCM v1 uses for npm credentials, so existing
// .ocmconfig credentials keep working.
const (
	credentialKeyUsername = "username"
	credentialKeyPassword = "password"
	credentialKeyEmail    = "email"
	credentialKeyToken    = "token"
)

var convertScheme = runtime.NewScheme()

func init() {
	MustRegisterCredentialType(convertScheme)
	directcredsv1.MustRegister(convertScheme)
}

// fromDirectCredentials converts a DirectCredentials properties map into typed NPMCredentials.
func fromDirectCredentials(properties map[string]string) *NPMCredentials {
	return &NPMCredentials{
		Type:     NPMCredentialsVersionedType,
		Username: properties[credentialKeyUsername],
		Password: properties[credentialKeyPassword],
		Email:    properties[credentialKeyEmail],
		Token:    properties[credentialKeyToken],
	}
}

// ConvertToNPMCredentials converts runtime.Typed into NPMCredentials.
// Direct conversion as well as converting from Credentials/v1 is supported.
func ConvertToNPMCredentials(creds runtime.Typed) (*NPMCredentials, error) {
	typed, err := convertScheme.NewObject(creds.GetType())
	if err != nil {
		return nil, fmt.Errorf("unsupported npm credential type: %w", err)
	}

	if err := convertScheme.Convert(creds, typed); err != nil {
		return nil, fmt.Errorf("cannot decode npm credentials: %w", err)
	}

	switch c := typed.(type) {
	case *NPMCredentials:
		return c, nil
	case *directcredsv1.DirectCredentials:
		return fromDirectCredentials(c.Properties), nil
	default:
		return nil, fmt.Errorf("unsupported npm credential type %v", typed.GetType())
	}
}
