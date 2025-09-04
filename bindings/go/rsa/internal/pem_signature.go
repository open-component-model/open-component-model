// Package signature provides helpers to encode and decode RSA-PSS signatures
// and optional certificate chains in PEM form.
package internal

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
)

// SignaturePEMBlockType is the PEM block type for raw signature bytes.
const SignaturePEMBlockType = "SIGNATURE"

// SignaturePEMBlockAlgorithmHeader is an optional PEM header that records the
// signature algorithm used for the SIGNATURE block, for example "RSASSA-PSS".
const SignaturePEMBlockAlgorithmHeader = "Signature Algorithm"

// SignatureBytesToPem encodes a signature and an optional certificate chain to PEM.
//
// Layout:
//   - One PEM block of type SIGNATURE that contains the raw signature bytes.
//   - Zero or more PEM blocks of type CERTIFICATE that form a chain.
//
// If algo is non-empty it is written into the SIGNATURE block headers using
// SignaturePEMBlockAlgorithmHeader.
func SignatureBytesToPem(algo string, data []byte, certs ...*x509.Certificate) []byte {
	block := &pem.Block{Type: SignaturePEMBlockType, Bytes: data}
	if algo != "" {
		block.Headers = map[string]string{SignaturePEMBlockAlgorithmHeader: algo}
	}
	return append(pem.EncodeToMemory(block), CertificateChainToPem(certs)...)
}

// CertificateChainToPem encodes a slice of X.509 certificates into consecutive
// CERTIFICATE PEM blocks. Order is preserved.
func CertificateChainToPem(certs []*x509.Certificate) []byte {
	var out []byte
	for _, c := range certs {
		out = append(out, pem.EncodeToMemory(&pem.Block{
			Type:  CertificatePEMBlockType,
			Bytes: c.Raw,
		})...,
		)
	}
	return out
}

// ErrNoPEM indicates the input contained no PEM blocks at all.
var ErrNoPEM = errors.New("pem: no data")

// GetSignatureFromPem extracts the first SIGNATURE block and its optional
// algorithm header from a concatenated PEM input, followed by any CERTIFICATE
// blocks as a chain.
//
// Returns:
//   - signature: the bytes from the first SIGNATURE block if present, otherwise nil
//   - algo: the value of SignaturePEMBlockAlgorithmHeader if present
//   - chain: parsed certificates that follow (or are present in the input)
//   - err: parsing errors (including malformed PEM or certificates)
//
// Empty pemData returns all-zero values and no error.
func GetSignatureFromPem(pemData []byte) ([]byte, string, []*x509.Certificate, error) {
	if len(pemData) == 0 {
		return nil, "", nil, nil
	}

	// Decode the first block to detect a SIGNATURE. If it is not a SIGNATURE,
	// we leave signature empty and parse certificates from the whole input.
	first, rest := pem.Decode(pemData)
	if first == nil {
		return nil, "", nil, ErrNoPEM
	}

	var sig []byte
	var algo string
	var chainSrc []byte

	if first.Type == SignaturePEMBlockType {
		sig = first.Bytes
		algo = first.Headers[SignaturePEMBlockAlgorithmHeader]
		chainSrc = rest
	} else {
		// No signature block up front. Parse certificates from the full input.
		chainSrc = pemData
	}

	certs, err := ParseCertificateChain(chainSrc)
	if err != nil {
		return nil, "", nil, fmt.Errorf("parse certificate chain: %w", err)
	}
	return sig, algo, certs, nil
}
