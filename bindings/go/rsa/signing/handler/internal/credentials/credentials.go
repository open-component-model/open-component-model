package credentials

import (
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"os"

	rsapem "ocm.software/open-component-model/bindings/go/rsa/signing/handler/internal/pem"
	identityv1 "ocm.software/open-component-model/bindings/go/rsa/spec/identity/v1"
)

// IdentityTypeRSA is the consumer identity type for RSA signing.
//
// Deprecated: Use identityv1.VersionedType or identityv1.V1Alpha1Type instead.
var IdentityTypeRSA = identityv1.V1Alpha1Type

// Credential keys.
//
//nolint:gosec // these are not secrets
const (
	CredentialKeyPublicKeyPEM      = "public_key_pem" // inline PEM
	CredentialKeyPublicKeyPEMFile  = CredentialKeyPublicKeyPEM + "_file"
	CredentialKeyPrivateKeyPEM     = "private_key_pem" // inline PEM
	CredentialKeyPrivateKeyPEMFile = CredentialKeyPrivateKeyPEM + "_file"
)

func PrivateKeyFromCredentials(credentials map[string]string) (*rsa.PrivateKey, error) {
	val := credentials[CredentialKeyPrivateKeyPEM]
	b, err := loadBytes(val, CredentialKeyPrivateKeyPEMFile, credentials)
	if err != nil {
		return nil, fmt.Errorf("failed loading private key PEM: %w", err)
	}
	if len(b) == 0 {
		return nil, nil
	}
	return rsapem.ParseRSAPrivateKeyPEM(b), nil
}

func PublicKeyFromCredentials(credentials map[string]string) (*rsapem.RSAPublicKeyPEM, error) {
	val := credentials[CredentialKeyPublicKeyPEM]
	b, err := loadBytes(val, CredentialKeyPublicKeyPEMFile, credentials)
	if err != nil {
		return nil, fmt.Errorf("failed loading public key PEM: %w", err)
	}
	if len(b) == 0 {
		// fallback: derive from private
		pk, err := PrivateKeyFromCredentials(credentials)
		if err != nil {
			return nil, err
		}
		if pk == nil {
			return nil, nil
		}
		return &rsapem.RSAPublicKeyPEM{
			PublicKey: &pk.PublicKey,
		}, nil
	}
	return rsapem.ParseRSAPublicKeyPEM(b), nil
}

func CertificateChainFromCredentials(credentials map[string]string) ([]*x509.Certificate, error) {
	val := credentials[CredentialKeyPublicKeyPEM]
	b, err := loadBytes(val, CredentialKeyPublicKeyPEMFile, credentials)
	if err != nil || len(b) == 0 {
		return nil, nil
	}
	return rsapem.ParseCertificateChain(b)
}

// loadBytes loads from file or inline PEM
func loadBytes(val string, fileKey string, credentials map[string]string) ([]byte, error) {
	if val != "" {
		// treat as literal bytes
		return []byte(val), nil
	}
	if path := credentials[fileKey]; path != "" {
		return os.ReadFile(path)
	}
	return nil, nil
}
