package v1

import (
	"fmt"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// OCICredentialsType is the type name for OCI registry credentials.
	OCICredentialsType = "OCICredentials"

	// CredentialKeyUsername is the key for basic auth username.
	CredentialKeyUsername = "username"
	// CredentialKeyPassword is the key for basic auth password.
	CredentialKeyPassword = "password"
	// CredentialKeyAccessToken is the key for OAuth2/bearer access tokens.
	CredentialKeyAccessToken = "accessToken"
	// CredentialKeyRefreshToken is the key for OAuth2 refresh tokens.
	CredentialKeyRefreshToken = "refreshToken"
)

var convertScheme = runtime.NewScheme()

func init() {
	convertScheme.MustRegisterWithAlias(&OCICredentials{},
		DockerConfigVersionedType,
		runtime.NewUnversionedType(DockerConfigType),
		OCICredentialsVersionedType,
		runtime.NewUnversionedType(OCICredentialsType),
	)
	v1.MustRegister(convertScheme)
}

// fromDirectCredentials converts a DirectCredentials properties map into typed OCICredentials.
// This supports old .ocmconfig files that use Credentials/v1 with OCI registry properties.
func fromDirectCredentials(properties map[string]string) *OCICredentials {
	return &OCICredentials{
		Type:         runtime.NewVersionedType(OCICredentialsType, Version),
		Username:     properties[CredentialKeyUsername],
		Password:     properties[CredentialKeyPassword],
		AccessToken:  properties[CredentialKeyAccessToken],
		RefreshToken: properties[CredentialKeyRefreshToken],
	}
}

// ConvertToOCICredentials converts runtime.Typed into OCICredentials.
// Direct conversation as well as converting from v1.DirectCredentials is supported.
// In every other case, an error will be returned.
func ConvertToOCICredentials(creds runtime.Typed) (*OCICredentials, error) {
	typed, err := convertScheme.NewObject(creds.GetType())
	if err != nil {
		return nil, fmt.Errorf("error converting credential type: %w", err)
	}

	if err = convertScheme.Convert(creds, typed); err != nil {
		return nil, fmt.Errorf("error converting credential type: %w", err)
	}

	switch t := typed.(type) {
	case *v1.DirectCredentials:
		return fromDirectCredentials(t.Properties), nil
	case *OCICredentials:
		return t, nil
	}

	return nil, fmt.Errorf("unsupported credential type %v", typed.GetType())
}
