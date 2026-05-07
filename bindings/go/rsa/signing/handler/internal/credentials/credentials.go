package credentials

import (
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"os"

	rsapem "ocm.software/open-component-model/bindings/go/rsa/signing/handler/internal/pem"
	rsacredentialsv1 "ocm.software/open-component-model/bindings/go/rsa/spec/credentials/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/rsa/spec/identity/v1"
)

// IdentityTypeRSA is the consumer identity type for RSA signing.
//
// Deprecated: Use identityv1.VersionedType or identityv1.V1Alpha1Type instead.
var IdentityTypeRSA = identityv1.V1Alpha1Type

func PrivateKeyFromCredentials(creds *rsacredentialsv1.RSACredentials) (*rsa.PrivateKey, error) {
	b, err := loadBytes(creds.PrivateKeyPEM, creds.PrivateKeyPEMFile)
	if err != nil {
		return nil, fmt.Errorf("failed loading private key PEM: %w", err)
	}
	if len(b) == 0 {
		return nil, nil
	}
	return rsapem.ParseRSAPrivateKeyPEM(b), nil
}

func PublicKeyFromCredentials(creds *rsacredentialsv1.RSACredentials) (*rsapem.RSAPublicKeyPEM, error) {
	b, err := loadBytes(creds.PublicKeyPEM, creds.PublicKeyPEMFile)
	if err != nil {
		return nil, fmt.Errorf("failed loading public key PEM: %w", err)
	}
	if len(b) == 0 {
		// fallback: derive from private
		pk, err := PrivateKeyFromCredentials(creds)
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

func CertificateChainFromCredentials(creds *rsacredentialsv1.RSACredentials) ([]*x509.Certificate, error) {
	b, err := loadBytes(creds.PublicKeyPEM, creds.PublicKeyPEMFile)
	if err != nil || len(b) == 0 {
		return nil, nil
	}
	return rsapem.ParseCertificateChain(b)
}

func loadBytes(inline, file string) ([]byte, error) {
	if inline != "" {
		// treat as literal bytes
		return []byte(inline), nil
	}
	if file != "" {
		return os.ReadFile(file)
	}
	return nil, nil
}
