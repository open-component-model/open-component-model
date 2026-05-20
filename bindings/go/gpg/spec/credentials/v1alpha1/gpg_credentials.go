package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// GPGCredentialsType is the type name for GPG credentials.
	GPGCredentialsType = "GPGCredentials"
	// Version is the version of the GPG credentials type.
	Version = "v1"
)

//nolint:gosec // G101: These are key names, not credentials.
const (
	CredentialKeyPrivateKeyPGP     = "privateKeyPGP"
	CredentialKeyPrivateKeyPGPFile = "privateKeyPGPFile"
	CredentialKeyPublicKeyPGP      = "publicKeyPGP"
	CredentialKeyPublicKeyPGPFile  = "publicKeyPGPFile"
	CredentialKeyPassphrase        = "passphrase"
)

// GPGCredentials represents typed credentials for GPG signing and verification.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type GPGCredentials struct {
	// +ocm:jsonschema-gen:enum=GPGCredentials/v1
	// +ocm:jsonschema-gen:enum:deprecated=GPGCredentials
	Type              runtime.Type `json:"type"`
	PrivateKeyPGP     string       `json:"privateKeyPGP,omitempty"`
	PrivateKeyPGPFile string       `json:"privateKeyPGPFile,omitempty"`
	PublicKeyPGP      string       `json:"publicKeyPGP,omitempty"`
	PublicKeyPGPFile  string       `json:"publicKeyPGPFile,omitempty"`
	Passphrase        string       `json:"passphrase,omitempty"`
}

// MustRegisterCredentialType registers GPGCredentials/v1 in the given scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&GPGCredentials{},
		runtime.NewVersionedType(GPGCredentialsType, Version),
		runtime.NewUnversionedType(GPGCredentialsType),
	)
}

// FromDirectCredentials converts a DirectCredentials properties map into typed GPGCredentials.
// This supports .ocmconfig files that use Credentials/v1 with GPG properties.
// A nil map is safe and returns a GPGCredentials with only the type set.
func FromDirectCredentials(properties map[string]string) *GPGCredentials {
	return &GPGCredentials{
		Type:              runtime.NewVersionedType(GPGCredentialsType, Version),
		PrivateKeyPGP:     properties[CredentialKeyPrivateKeyPGP],
		PrivateKeyPGPFile: properties[CredentialKeyPrivateKeyPGPFile],
		PublicKeyPGP:      properties[CredentialKeyPublicKeyPGP],
		PublicKeyPGPFile:  properties[CredentialKeyPublicKeyPGPFile],
		Passphrase:        properties[CredentialKeyPassphrase],
	}
}
