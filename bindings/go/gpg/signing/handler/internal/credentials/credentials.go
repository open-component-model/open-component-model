package credentials

import (
	"bytes"
	"fmt"
	"os"

	"github.com/ProtonMail/go-crypto/openpgp"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var IdentityTypeGPG = runtime.NewVersionedType("GPG", "v1alpha1")

// Credential keys.
//
//nolint:gosec // these are not secrets
const (
	CredentialKeyPrivateKeyPGP     = "private_key_pgp"
	CredentialKeyPrivateKeyPGPFile = CredentialKeyPrivateKeyPGP + "_file"
	CredentialKeyPublicKeyPGP      = "public_key_pgp"
	CredentialKeyPublicKeyPGPFile  = CredentialKeyPublicKeyPGP + "_file"
	CredentialKeyPassphrase        = "passphrase"
)

// PrivateEntityFromCredentials loads a signing-capable OpenPGP entity from
// the credential map, decrypting it with the passphrase credential if present.
func PrivateEntityFromCredentials(creds map[string]string) (*openpgp.Entity, error) {
	b, err := loadBytes(creds[CredentialKeyPrivateKeyPGP], CredentialKeyPrivateKeyPGPFile, creds)
	if err != nil {
		return nil, fmt.Errorf("load private key: %w", err)
	}
	if len(b) == 0 {
		return nil, nil
	}

	entities, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("parse armored private key: %w", err)
	}
	if len(entities) == 0 {
		return nil, fmt.Errorf("no keys found in private key material")
	}

	entity := entities[0]

	if passphrase := creds[CredentialKeyPassphrase]; passphrase != "" {
		if err := entity.DecryptPrivateKeys([]byte(passphrase)); err != nil {
			return nil, fmt.Errorf("decrypt GPG private key: %w", err)
		}
	}

	return entity, nil
}

// PublicKeyRingFromCredentials loads a public OpenPGP key ring from credentials.
// Falls back to the private key if no public key is provided.
func PublicKeyRingFromCredentials(creds map[string]string) (openpgp.EntityList, error) {
	b, err := loadBytes(creds[CredentialKeyPublicKeyPGP], CredentialKeyPublicKeyPGPFile, creds)
	if err != nil {
		return nil, fmt.Errorf("load public key: %w", err)
	}
	if len(b) == 0 {
		// fall back to private key material for the public key
		b, err = loadBytes(creds[CredentialKeyPrivateKeyPGP], CredentialKeyPrivateKeyPGPFile, creds)
		if err != nil {
			return nil, fmt.Errorf("load private key as fallback for verification: %w", err)
		}
	}
	if len(b) == 0 {
		return nil, nil
	}

	entities, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("parse armored public key: %w", err)
	}
	return entities, nil
}

func loadBytes(val string, fileKey string, creds map[string]string) ([]byte, error) {
	if val != "" {
		return []byte(val), nil
	}
	if path := creds[fileKey]; path != "" {
		return os.ReadFile(path)
	}
	return nil, nil
}
