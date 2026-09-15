package v1

import (
	"fmt"

	directv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	credentialKeyUsername      = "username"
	credentialKeyPassword      = "password"
	credentialKeyToken         = "token"
	credentialKeyPrivateKey    = "privateKey"
	credentialKeyPrivateKeyPEM = "privateKeyPEM"
)

var convertScheme = runtime.NewScheme()

func init() {
	MustRegisterCredentialType(convertScheme)
	directv1.MustRegister(convertScheme)
}

func fromDirectCredentials(properties map[string]string) *GitCredentials {
	return &GitCredentials{
		Type:          runtime.NewVersionedType(GitCredentialsType, Version),
		Username:      properties[credentialKeyUsername],
		Password:      properties[credentialKeyPassword],
		Token:         properties[credentialKeyToken],
		PrivateKey:    properties[credentialKeyPrivateKey],
		PrivateKeyPEM: properties[credentialKeyPrivateKeyPEM],
	}
}

func ConvertToGitCredentials(creds runtime.Typed) (*GitCredentials, error) {
	typed, err := convertScheme.NewObject(creds.GetType())
	if err != nil {
		return nil, fmt.Errorf("unsupported git credential type: %w", err)
	}

	if err := convertScheme.Convert(creds, typed); err != nil {
		return nil, fmt.Errorf("cannot decode git credentials: %w", err)
	}

	switch c := typed.(type) {
	case *GitCredentials:
		return c, nil
	case *directv1.DirectCredentials:
		return fromDirectCredentials(c.Properties), nil
	default:
		return nil, fmt.Errorf("unsupported git credential type")
	}
}
