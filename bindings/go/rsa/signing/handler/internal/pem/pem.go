// Package internal contains low-level PEM and X.509 helpers used by the RSA-PSS
// handler. Functions here are intentionally small and dependency-free.
package pem

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

// PEM block types used across helpers.
const (
	CertificatePEMBlockType = "CERTIFICATE"
	pemPKCS1PrivateKey      = "RSA PRIVATE KEY"
	pemPKCS8PrivateKey      = "PRIVATE KEY"
	pemPKIXPublicKey        = "PUBLIC KEY"
	pemPKCS1PublicKey       = "RSA PUBLIC KEY"
)

// ParseRSAPrivateKeyPEM scans concatenated PEM data and returns the first RSA
// private key found. It supports PKCS#1 ("RSA PRIVATE KEY") and PKCS#8
// ("PRIVATE KEY") containers. It returns nil if no RSA key can be parsed.
func ParseRSAPrivateKeyPEM(pemBytes []byte) *rsa.PrivateKey {
	for len(pemBytes) > 0 {
		block, rest := pem.Decode(pemBytes)
		if block == nil {
			break
		}
		switch block.Type {
		case pemPKCS1PrivateKey:
			if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
				return k
			}
		case pemPKCS8PrivateKey:
			if anyKey, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
				if k, ok := anyKey.(*rsa.PrivateKey); ok {
					return k
				}
			}
		}
		pemBytes = rest
	}
	return nil
}

// ParseRSAPublicKeyPEM scans concatenated PEM data and returns the first RSA
// public key found plus the original parsed object it came from:
//   - For "PUBLIC KEY": returns (*rsa.PublicKey, crypto.PublicKey)
//   - For "RSA PUBLIC KEY": returns (*rsa.PublicKey, *rsa.PublicKey)
//   - For "CERTIFICATE": returns (*rsa.PublicKey, *x509.Certificate)
//
// If none can be parsed it returns (nil, nil).
func ParseRSAPublicKeyPEM(pemBytes []byte) (*rsa.PublicKey, any) {
	for len(pemBytes) > 0 {
		block, rest := pem.Decode(pemBytes)
		if block == nil {
			break
		}
		switch block.Type {
		case pemPKIXPublicKey:
			if k, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
				if pk, ok := k.(*rsa.PublicKey); ok {
					return pk, k
				}
			}
		case pemPKCS1PublicKey:
			if pk, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
				return pk, pk
			}
		case CertificatePEMBlockType:
			if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
				if pk, ok := cert.PublicKey.(*rsa.PublicKey); ok {
					return pk, cert
				}
			}
		}
		pemBytes = rest
	}
	return nil, nil
}

// ParseCertificateChain parses one or more consecutive CERTIFICATE PEM blocks
// and returns them in order. If a non-CERTIFICATE block is encountered before
// any certificate is parsed, or if no certificates are found, an error is
// returned.
func ParseCertificateChain(data []byte) ([]*x509.Certificate, error) {
	var chain []*x509.Certificate

	for len(data) > 0 {
		block, rest := pem.Decode(data)
		if block == nil {
			break
		}
		if block.Type != CertificatePEMBlockType {
			if len(chain) == 0 {
				return nil, fmt.Errorf("unexpected pem block type for certificate: %q", block.Type)
			}
			// Stop at first non-certificate after having parsed at least one.
			break
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		chain = append(chain, cert)
		data = rest
	}

	if len(chain) == 0 {
		return nil, fmt.Errorf("invalid certificate format (expected %q PEM block)", CertificatePEMBlockType)
	}
	return chain, nil
}
