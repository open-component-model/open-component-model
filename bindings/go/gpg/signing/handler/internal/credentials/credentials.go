package credentials

import (
	"bytes"
	"fmt"
	"os"

	"github.com/ProtonMail/go-crypto/openpgp"

	gpgcredentialsv1 "ocm.software/open-component-model/bindings/go/gpg/spec/credentials/v1alpha1"
	identityv1 "ocm.software/open-component-model/bindings/go/gpg/spec/identity/v1alpha1"
)

// IdentityTypeGPG is the consumer identity type for GPG signing.
//
// Deprecated: Use identityv1.VersionedType or identityv1.V1Alpha1Type instead.
var IdentityTypeGPG = identityv1.V1Alpha1Type

// PrivateEntityFromCredentials loads a signing-capable OpenPGP entity from
// the credential map, decrypting it with the passphrase credential if present.
// Returns only the first entity; use PrivateKeyRingFromCredentials for multi-key keyrings.
func PrivateEntityFromCredentials(creds *gpgcredentialsv1.GPGCredentials) (*openpgp.Entity, error) {
	if creds == nil {
		return nil, nil
	}
	entities, err := PrivateKeyRingFromCredentials(creds)
	if err != nil {
		return nil, err
	}
	if len(entities) == 0 {
		return nil, nil
	}
	return entities[0], nil
}

// PrivateKeyRingFromCredentials loads all signing-capable OpenPGP entities from
// the credential map, decrypting each with the passphrase credential if present.
func PrivateKeyRingFromCredentials(creds *gpgcredentialsv1.GPGCredentials) (openpgp.EntityList, error) {
	if creds == nil {
		return nil, nil
	}
	b, err := loadBytes(creds.PrivateKeyPGP, creds.PrivateKeyPGPFile)
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

	if creds.Passphrase != "" {
		for _, entity := range entities {
			if err := entity.DecryptPrivateKeys([]byte(creds.Passphrase)); err != nil {
				return nil, fmt.Errorf("decrypt GPG private key: %w", err)
			}
		}
	}

	return entities, nil
}

// PublicKeyRingFromCredentials loads a public OpenPGP key ring from credentials.
// Falls back to the private key if no public key is provided.
func PublicKeyRingFromCredentials(creds *gpgcredentialsv1.GPGCredentials) (openpgp.EntityList, error) {
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
	if len(b) == 0 {
		return nil, nil
	}

	entities, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("parse armored public key: %w", err)
	}
	return entities, nil
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
