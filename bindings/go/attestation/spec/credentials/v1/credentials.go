package v1

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
)

//nolint:gosec // G101: these are PEM block type names, not credentials.
const (
	ECDSAPrivateKeyPEMBlockType = "EC PRIVATE KEY"
	PKCS8PrivateKeyPEMBlockType = "PRIVATE KEY"
	PKIXPublicKeyPEMBlockType   = "PUBLIC KEY"
)

// ParsePrivateKeyPEM parses a PEM-encoded ECDSA P-256 private key. It supports
// PKCS#8 ("PRIVATE KEY") and SEC1 ("EC PRIVATE KEY") containers and verifies
// that the key uses the P-256 curve, matching what cosign expects for ECDSA
// signature verification.
func ParsePrivateKeyPEM(b []byte) (*ecdsa.PrivateKey, error) {
	for len(b) > 0 {
		block, rest := pem.Decode(b)
		if block == nil {
			break
		}
		var (
			key any
			err error
		)
		switch block.Type {
		case PKCS8PrivateKeyPEMBlockType:
			key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
		case ECDSAPrivateKeyPEMBlockType:
			key, err = x509.ParseECPrivateKey(block.Bytes)
		default:
			b = rest
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("parsing ECDSA private key failed: %w", err)
		}
		ecdsaKey, ok := key.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key is not an ECDSA key: %T", key)
		}
		if ecdsaKey.Curve != elliptic.P256() {
			return nil, fmt.Errorf("unsupported ECDSA curve %q: only P-256 is supported", ecdsaKey.Curve.Params().Name)
		}
		return ecdsaKey, nil
	}
	return nil, fmt.Errorf("no ECDSA private key found in PEM data")
}

// PrivateKeyFromCredentials resolves the ECDSA P-256 private key from the given
// credentials, preferring the inline PEM over the file path. It returns
// (nil, nil) when no private key is configured.
func PrivateKeyFromCredentials(creds *ECDSACredentials) (*ecdsa.PrivateKey, error) {
	if creds == nil {
		return nil, nil
	}
	b, err := loadBytes(creds.PrivateKeyPEM, creds.PrivateKeyPEMFile)
	if err != nil {
		return nil, fmt.Errorf("loading private key PEM failed: %w", err)
	}
	if len(b) == 0 {
		return nil, nil
	}
	return ParsePrivateKeyPEM(b)
}

// PublicPEM encodes the given ECDSA public key as a PKIX ("PUBLIC KEY") PEM
// block, suitable for use as a cosign `--key` file.
func PublicPEM(key *ecdsa.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshalling public key failed: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: PKIXPublicKeyPEMBlockType, Bytes: der}), nil
}

// PublicKeyPEMFromCredentials returns the PEM-encoded ECDSA public key of the
// given credentials, preferring an inline or file-based public key, and
// otherwise deriving it from the private key.
func PublicKeyPEMFromCredentials(creds *ECDSACredentials) ([]byte, error) {
	if creds == nil {
		return nil, nil
	}
	b, err := loadBytes(creds.PublicKeyPEM, creds.PublicKeyPEMFile)
	if err != nil {
		return nil, fmt.Errorf("loading public key PEM failed: %w", err)
	}
	if len(b) > 0 {
		return b, nil
	}
	key, err := PrivateKeyFromCredentials(creds)
	if err != nil {
		return nil, err
	}
	if key == nil {
		return nil, nil
	}
	return PublicPEM(&key.PublicKey)
}

func loadBytes(inline, file string) ([]byte, error) {
	if inline != "" {
		return []byte(inline), nil
	}
	if file != "" {
		return os.ReadFile(file)
	}
	return nil, nil
}
