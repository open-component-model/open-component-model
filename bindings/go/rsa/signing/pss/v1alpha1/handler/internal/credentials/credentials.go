package credentials

import (
	"crypto/rsa"
	"crypto/x509"
	"os"

	rsapem "ocm.software/open-component-model/bindings/go/rsa/internal"
)

// These constants describe credential keys for RSA-PSS signing.
const (
	CredentialKeyPublicKeyPEMFile  = "public_key_pem_file"
	CredentialKeyPrivateKeyPEMFile = "private_key_pem_file"
)

func PrivateKeyFromCredentials(credentials map[string]string) *rsa.PrivateKey {
	path := credentials[CredentialKeyPrivateKeyPEMFile]
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return rsapem.ParseRSAPrivateKeyPEM(b)
}

func PublicKeyFromCredentials(credentials map[string]string) (*rsa.PublicKey, any) {
	path := credentials[CredentialKeyPublicKeyPEMFile]
	if path == "" {
		// Fallback: derive public key from private key if only private is provided.
		if pk := PrivateKeyFromCredentials(credentials); pk != nil {
			return &pk.PublicKey, pk
		}
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}

	return rsapem.ParseRSAPublicKeyPEM(b)
}

func CertificateChainFromCredentials(credentials map[string]string) ([]*x509.Certificate, error) {
	path := credentials[CredentialKeyPublicKeyPEMFile]
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}

	return rsapem.ParseCertificateChain(b)
}
