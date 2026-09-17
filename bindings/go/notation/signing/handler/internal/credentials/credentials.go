// Package credentials loads Notation signing/verification key and trust
// material from typed NotationCredentials, honoring the inline-over-file
// precedence used across OCM credential types.
package credentials

import (
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"

	notationcredsv1 "ocm.software/open-component-model/bindings/go/notation/spec/credentials/v1"
)

// PrivateKeyFromCredentials parses the signing private key (PKCS#1 or PKCS#8)
// from credentials. Returns (nil, nil) when no key material is configured.
func PrivateKeyFromCredentials(creds *notationcredsv1.NotationCredentials) (crypto.PrivateKey, error) {
	if creds == nil {
		return nil, nil
	}
	b, err := loadBytes(creds.PrivateKeyPEM, creds.PrivateKeyPEMFile)
	if err != nil {
		return nil, fmt.Errorf("failed loading private key PEM: %w", err)
	}
	if len(b) == 0 {
		return nil, nil
	}
	return parsePrivateKey(b)
}

// CertificateChainFromCredentials parses the signer certificate chain (leaf
// first, then intermediates) from credentials. Returns (nil, nil) when no
// chain is configured.
func CertificateChainFromCredentials(creds *notationcredsv1.NotationCredentials) ([]*x509.Certificate, error) {
	if creds == nil {
		return nil, nil
	}
	b, err := loadBytes(creds.CertificateChainPEM, creds.CertificateChainPEMFile)
	if err != nil {
		return nil, fmt.Errorf("failed loading certificate chain PEM: %w", err)
	}
	if len(b) == 0 {
		return nil, nil
	}
	return parseCertificates(b)
}

// TrustedCACertificatesFromCredentials parses the verifier trust anchors from
// credentials. Returns (nil, nil) when no CA material is configured.
func TrustedCACertificatesFromCredentials(creds *notationcredsv1.NotationCredentials) ([]*x509.Certificate, error) {
	if creds == nil {
		return nil, nil
	}
	b, err := loadBytes(creds.TrustedCACertificatesPEM, creds.TrustedCACertificatesPEMFile)
	if err != nil {
		return nil, fmt.Errorf("failed loading trusted CA certificates PEM: %w", err)
	}
	if len(b) == 0 {
		return nil, nil
	}
	return parseCertificates(b)
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

// parsePrivateKey parses the first private-key PEM block, trying PKCS#8 then
// PKCS#1 (RSA) then SEC1 (EC).
func parsePrivateKey(b []byte) (crypto.PrivateKey, error) {
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in private key material")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, fmt.Errorf("unsupported private key format (want PKCS#8, PKCS#1, or SEC1 EC)")
}

// parseCertificates parses every CERTIFICATE PEM block in b, preserving order.
func parseCertificates(b []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	rest := b
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse certificate: %w", err)
		}
		certs = append(certs, cert)
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("no CERTIFICATE PEM block found")
	}
	return certs, nil
}
