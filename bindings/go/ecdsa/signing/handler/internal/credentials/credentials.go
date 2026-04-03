package credentials

import (
	"crypto/ecdsa"
	"crypto/x509"
	"fmt"
	"os"

	ecdsapem "ocm.software/open-component-model/bindings/go/ecdsa/signing/handler/internal/pem"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var IdentityTypeECDSA = runtime.NewVersionedType("ECDSA", "v1alpha1")

// Credential keys.
//
//nolint:gosec // these are not secrets
const (
	CredentialKeyPublicKeyPEM      = "public_key_pem" // inline PEM
	CredentialKeyPublicKeyPEMFile  = CredentialKeyPublicKeyPEM + "_file"
	CredentialKeyPrivateKeyPEM     = "private_key_pem" // inline PEM
	CredentialKeyPrivateKeyPEMFile = CredentialKeyPrivateKeyPEM + "_file"
)

func PrivateKeyFromCredentials(credentials map[string]string) (*ecdsa.PrivateKey, error) {
	val := credentials[CredentialKeyPrivateKeyPEM]
	b, err := loadBytes(val, CredentialKeyPrivateKeyPEMFile, credentials)
	if err != nil {
		return nil, fmt.Errorf("failed loading private key PEM: %w", err)
	}
	if len(b) == 0 {
		return nil, nil
	}
	return ecdsapem.ParseECDSAPrivateKeyPEM(b), nil
}

func PublicKeyFromCredentials(credentials map[string]string) (*ecdsapem.ECDSAPublicKeyPEM, error) {
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
		return &ecdsapem.ECDSAPublicKeyPEM{
			PublicKey: &pk.PublicKey,
		}, nil
	}
	return ecdsapem.ParseECDSAPublicKeyPEM(b), nil
}

func CertificateChainFromCredentials(credentials map[string]string) ([]*x509.Certificate, error) {
	val := credentials[CredentialKeyPublicKeyPEM]
	b, err := loadBytes(val, CredentialKeyPublicKeyPEMFile, credentials)
	if err != nil || len(b) == 0 {
		return nil, nil
	}
	return ecdsapem.ParseCertificateChain(b)
}

func loadBytes(val string, fileKey string, credentials map[string]string) ([]byte, error) {
	if val != "" {
		return []byte(val), nil
	}
	if path := credentials[fileKey]; path != "" {
		return os.ReadFile(path)
	}
	return nil, nil
}
