package v1

import (
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

	// LegacyCredentialKeyAccessToken is the legacy snake_case key for access tokens.
	//
	// Deprecated: Use CredentialKeyAccessToken instead.
	LegacyCredentialKeyAccessToken = "access_token"
	// LegacyCredentialKeyRefreshToken is the legacy snake_case key for refresh tokens.
	//
	// Deprecated: Use CredentialKeyRefreshToken instead.
	LegacyCredentialKeyRefreshToken = "refresh_token"
)

// OCICredentials represents typed credentials for OCI registry authentication.
// It supports username/password and token-based authentication flows used by
// container registries that implement the OCI distribution specification.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type OCICredentials struct {
	// +ocm:jsonschema-gen:enum=OCICredentials/v1
	Type         runtime.Type `json:"type"`
	Username     string       `json:"username,omitempty"`
	Password     string       `json:"password,omitempty"`
	AccessToken  string       `json:"accessToken,omitempty"`
	RefreshToken string       `json:"refreshToken,omitempty"`
}

// MustRegisterCredentialType registers OCICredentials/v1 in the given scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&OCICredentials{},
		runtime.NewVersionedType(OCICredentialsType, Version),
		runtime.NewUnversionedType(OCICredentialsType),
	)
}

// FromDirectCredentials converts a DirectCredentials properties map into typed OCICredentials.
// This supports old .ocmconfig files that use Credentials/v1 with OCI registry properties.
// It handles both canonical camelCase keys and legacy snake_case keys, with camelCase
// taking precedence.
func FromDirectCredentials(properties map[string]string) *OCICredentials {
	return &OCICredentials{
		Type:         runtime.NewVersionedType(OCICredentialsType, Version),
		Username:     properties[CredentialKeyUsername],
		Password:     properties[CredentialKeyPassword],
		AccessToken:  stringWithFallback(properties, CredentialKeyAccessToken, LegacyCredentialKeyAccessToken),
		RefreshToken: stringWithFallback(properties, CredentialKeyRefreshToken, LegacyCredentialKeyRefreshToken),
	}
}

func stringWithFallback(m map[string]string, key, fallback string) string {
	if v, ok := m[key]; ok {
		return v
	}
	return m[fallback]
}
