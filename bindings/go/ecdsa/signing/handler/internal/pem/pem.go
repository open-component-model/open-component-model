// Package pem contains low-level PEM and X.509 helpers used by the ECDSA handler.
package pem

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

// PEM block types used across helpers.
const (
	CertificatePEMBlockType = "CERTIFICATE"
	pemECPrivateKey         = "EC PRIVATE KEY"
	pemPKCS8PrivateKey      = "PRIVATE KEY"
	pemPKIXPublicKey        = "PUBLIC KEY"
)

// ParseECDSAPrivateKeyPEM scans concatenated PEM data and returns the first ECDSA
// private key found. It supports SEC 1 ("EC PRIVATE KEY") and PKCS#8
// ("PRIVATE KEY") containers. It returns nil if no ECDSA key can be parsed.
func ParseECDSAPrivateKeyPEM(pemBytes []byte) *ecdsa.PrivateKey {
	for len(pemBytes) > 0 {
		block, rest := pem.Decode(pemBytes)
		if block == nil {
			break
		}
		switch block.Type {
		case pemECPrivateKey:
			if k, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
				return k
			}
		case pemPKCS8PrivateKey:
			if anyKey, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
				if k, ok := anyKey.(*ecdsa.PrivateKey); ok {
					return k
				}
			}
		}
		pemBytes = rest
	}
	return nil
}

// ECDSAPublicKeyPEM holds a parsed ECDSA public key and optionally the original
// X.509 certificate it came from.
type ECDSAPublicKeyPEM struct {
	PublicKey      *ecdsa.PublicKey
	UnderlyingCert *x509.Certificate
}

func (p *ECDSAPublicKeyPEM) GetOptionalUnderlyingCert() *x509.Certificate {
	if p == nil {
		return nil
	}
	return p.UnderlyingCert
}

// ParseECDSAPublicKeyPEM scans concatenated PEM data and returns ECDSAPublicKeyPEM
// if one can be parsed. It supports PKIX ("PUBLIC KEY") containers as well as
// X.509 certificates containing ECDSA public keys.
//
// If none can be parsed it returns nil.
func ParseECDSAPublicKeyPEM(pemBytes []byte) *ECDSAPublicKeyPEM {
	for len(pemBytes) > 0 {
		block, rest := pem.Decode(pemBytes)
		if block == nil {
			return nil
		}
		switch block.Type {
		case pemPKIXPublicKey:
			if k, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
				if pk, ok := k.(*ecdsa.PublicKey); ok {
					return &ECDSAPublicKeyPEM{
						PublicKey: pk,
					}
				}
			}
		case CertificatePEMBlockType:
			if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
				if pk, ok := cert.PublicKey.(*ecdsa.PublicKey); ok {
					return &ECDSAPublicKeyPEM{
						PublicKey:      pk,
						UnderlyingCert: cert,
					}
				}
			}
		}
		pemBytes = rest
	}
	return nil
}

// ParseCertificateChain parses one or more consecutive CERTIFICATE PEM blocks
// and returns them in order.
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
