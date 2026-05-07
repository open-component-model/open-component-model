package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// RSACredentialsType is the type name for RSA credentials.
	RSACredentialsType = "RSACredentials"
	// Version is the version of the RSA credentials type.
	Version = "v1"
)

// Credential keys match the existing snake_case keys used in .ocmconfig files.
//
//nolint:gosec // G101: These are key names, not credentials.
const (
	CredentialKeyPublicKeyPEM      = "public_key_pem"
	CredentialKeyPublicKeyPEMFile  = CredentialKeyPublicKeyPEM + "_file"
	CredentialKeyPrivateKeyPEM     = "private_key_pem"
	CredentialKeyPrivateKeyPEMFile = CredentialKeyPrivateKeyPEM + "_file"
)

// RSACredentials represents typed credentials for RSA signing and verification.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type RSACredentials struct {
	// +ocm:jsonschema-gen:enum=RSACredentials/v1
	// +ocm:jsonschema-gen:enum:deprecated=RSACredentials
	Type              runtime.Type `json:"type"`
	PublicKeyPEM      string       `json:"publicKeyPEM,omitempty"`
	PublicKeyPEMFile  string       `json:"publicKeyPEMFile,omitempty"`
	PrivateKeyPEM     string       `json:"privateKeyPEM,omitempty"`
	PrivateKeyPEMFile string       `json:"privateKeyPEMFile,omitempty"`
}

// MustRegisterCredentialType registers RSACredentials/v1 in the given scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&RSACredentials{},
		runtime.NewVersionedType(RSACredentialsType, Version),
		runtime.NewUnversionedType(RSACredentialsType),
	)
}

// FromDirectCredentials converts a DirectCredentials properties map into typed RSACredentials.
// This supports old .ocmconfig files that use Credentials/v1 with RSA properties.
// A nil map is safe and returns an RSACredentials with only the type set.
func FromDirectCredentials(properties map[string]string) *RSACredentials {
	return &RSACredentials{
		Type:              runtime.NewVersionedType(RSACredentialsType, Version),
		PublicKeyPEM:      properties[CredentialKeyPublicKeyPEM],
		PublicKeyPEMFile:  properties[CredentialKeyPublicKeyPEMFile],
		PrivateKeyPEM:     properties[CredentialKeyPrivateKeyPEM],
		PrivateKeyPEMFile: properties[CredentialKeyPrivateKeyPEMFile],
	}
}
