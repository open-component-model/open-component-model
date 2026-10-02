package credentials

import (
	"fmt"
	"os"

	gpgcredentialsv1 "ocm.software/open-component-model/bindings/go/gpg/spec/credentials/v1alpha1"
	identityv1 "ocm.software/open-component-model/bindings/go/gpg/spec/identity/v1alpha1"
)

// IdentityTypeGPG is the consumer identity type for GPG signing.
var IdentityTypeGPG = identityv1.V1Alpha1Type

// PrivateKeyBytes returns the private key material from the inline value or the file.
func PrivateKeyBytes(creds *gpgcredentialsv1.GPGCredentials) ([]byte, error) {
	if creds == nil {
		return nil, nil
	}
	b, err := loadBytes(creds.PrivateKeyPGP, creds.PrivateKeyPGPFile)
	if err != nil {
		return nil, fmt.Errorf("load private key: %w", err)
	}
	return b, nil
}

// PublicKeyBytes returns the public key material, falling back to the private key material.
func PublicKeyBytes(creds *gpgcredentialsv1.GPGCredentials) ([]byte, error) {
	if creds == nil {
		return nil, nil
	}
	b, err := loadBytes(creds.PublicKeyPGP, creds.PublicKeyPGPFile)
	if err != nil {
		return nil, fmt.Errorf("load public key: %w", err)
	}
	if len(b) == 0 {
		// fall back to private key material for the public key
		b, err = loadBytes(creds.PrivateKeyPGP, creds.PrivateKeyPGPFile)
		if err != nil {
			return nil, fmt.Errorf("load private key as fallback for verification: %w", err)
		}
	}
	return b, nil
}

func loadBytes(val string, file string) ([]byte, error) {
	if val != "" {
		return []byte(val), nil
	}
	if file != "" {
		return os.ReadFile(file)
	}
	return nil, nil
}
