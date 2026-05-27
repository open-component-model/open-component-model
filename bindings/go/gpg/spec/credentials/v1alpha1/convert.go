package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

//nolint:gosec // G101: These are key names, not credentials.
const (
	credentialKeyPrivateKeyPGP     = "privateKeyPGP"
	credentialKeyPrivateKeyPGPFile = "privateKeyPGPFile"
	credentialKeyPublicKeyPGP      = "publicKeyPGP"
	credentialKeyPublicKeyPGPFile  = "publicKeyPGPFile"
	credentialKeyPassphrase        = "passphrase"
)

// FromDirectCredentials converts a DirectCredentials properties map into typed GPGCredentials.
// This supports .ocmconfig files that use Credentials/v1 with GPG properties.
// A nil map is safe and returns a GPGCredentials with only the type set.
func FromDirectCredentials(properties map[string]string) *GPGCredentials {
	return &GPGCredentials{
		Type:              runtime.NewVersionedType(GPGCredentialsType, Version),
		PrivateKeyPGP:     properties[credentialKeyPrivateKeyPGP],
		PrivateKeyPGPFile: properties[credentialKeyPrivateKeyPGPFile],
		PublicKeyPGP:      properties[credentialKeyPublicKeyPGP],
		PublicKeyPGPFile:  properties[credentialKeyPublicKeyPGPFile],
		Passphrase:        properties[credentialKeyPassphrase],
	}
}
