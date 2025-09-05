package credentials

import (
	"crypto/rsa"
	"crypto/x509"
	"os"

	rsapem "ocm.software/open-component-model/bindings/go/rsa/internal"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var IdentityTypeRSA = runtime.NewVersionedType("RSA", "v1alpha1")

// Credential keys.
const (
	CredentialKeyPublicKeyPEM      = "public_key_pem" // inline PEM
	CredentialKeyPublicKeyPEMFile  = CredentialKeyPublicKeyPEM + "_file"
	CredentialKeyPrivateKeyPEM     = "private_key_pem" // inline PEM
	CredentialKeyPrivateKeyPEMFile = CredentialKeyPrivateKeyPEM + "_file"
)

func PrivateKeyFromCredentials(credentials map[string]string) *rsa.PrivateKey {
	val := credentials[CredentialKeyPrivateKeyPEM]
	b, err := loadBytes(val, CredentialKeyPrivateKeyPEMFile, credentials)
	if err != nil || len(b) == 0 {
		return nil
	}
	return rsapem.ParseRSAPrivateKeyPEM(b)
}

func PublicKeyFromCredentials(credentials map[string]string) (*rsa.PublicKey, any) {
	val := credentials[CredentialKeyPublicKeyPEM]
	b, err := loadBytes(val, CredentialKeyPublicKeyPEMFile, credentials)
	if err != nil || len(b) == 0 {
		// fallback: derive from private
		if pk := PrivateKeyFromCredentials(credentials); pk != nil {
			return &pk.PublicKey, pk
		}
		return nil, nil
	}
	return rsapem.ParseRSAPublicKeyPEM(b)
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
