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
	MustRegisterTransportCredentialTypes(convertScheme)
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

// ConvertCredentials decodes one of the supported Git credential types.
// Explicit transport credentials reject unknown fields and are validated before use.
// Legacy credentials are normalized with their original authentication precedence.
func ConvertCredentials(creds runtime.Typed) (runtime.Typed, error) {
	if creds == nil {
		return nil, nil
	}
	typed, err := convertScheme.NewObject(creds.GetType())
	if err != nil {
		return nil, fmt.Errorf("unsupported git credential type: %w", err)
	}
	switch typed.(type) {
	case *GitCredentials, *directv1.DirectCredentials:
		legacy, err := ConvertToGitCredentials(creds)
		if err != nil {
			return nil, err
		}
		return convertLegacyCredentials(legacy)
	}
	if err := runtime.DecodeStrict(creds, typed); err != nil {
		return nil, fmt.Errorf("cannot decode git credentials: %w", err)
	}
	if validator, ok := typed.(runtime.Validatable); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("invalid git credentials: %w", err)
		}
	}
	return typed, nil
}

// convertLegacyCredentials preserves legacy precedence while selecting one explicit method.
func convertLegacyCredentials(creds *GitCredentials) (runtime.Typed, error) {
	switch {
	case creds.PrivateKeyPEM != "" || creds.PrivateKey != "":
		keyPath := creds.PrivateKey
		if creds.PrivateKeyPEM != "" {
			keyPath = ""
		}
		return &GitSSHCredentials{
			Type:          runtime.NewVersionedType(GitSSHCredentialsType, Version),
			Username:      creds.Username,
			PrivateKey:    keyPath,
			PrivateKeyPEM: creds.PrivateKeyPEM,
			Passphrase:    creds.Password,
		}, nil
	case creds.Token != "":
		return &GitBearerCredentials{Type: runtime.NewVersionedType(GitBearerCredentialsType, Version), Token: creds.Token}, nil
	case creds.Username != "":
		return &GitHTTPSCredentials{Type: runtime.NewVersionedType(GitHTTPSCredentialsType, Version), Username: creds.Username, Password: creds.Password}, nil
	case creds.Password != "":
		return nil, fmt.Errorf("password requires a username or SSH private key")
	default:
		return nil, nil
	}
}
