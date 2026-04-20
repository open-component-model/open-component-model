package internal

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// TrustedRootParams contains all material needed to build a trusted_root.json.
type TrustedRootParams struct {
	FulcioRootPEM     []byte
	RekorPublicKeyPEM []byte
	RekorOrigin       string // checkpoint origin (--hostname flag), e.g. "rekor-local"
	RekorBaseURL      string // base URL for the Rekor tlog entry in the trusted root, e.g. "http://rekor-server:3000"
	CTLogPublicKeyDER []byte
	CTLogID           [sha256.Size]byte
	TSACertChainPEM   []byte
}

// BuildTrustedRoot generates a trusted_root.json file that covers Fulcio CA,
// Rekor tlog, CT log, and timestamp authority. It writes the file to tmpDir.
func BuildTrustedRoot(tmpDir string, params TrustedRootParams) (string, error) {
	fulcioCertDER, err := pemToDER(params.FulcioRootPEM, "CERTIFICATE")
	if err != nil {
		return "", fmt.Errorf("decode fulcio cert: %w", err)
	}
	rekorKeyDER, err := pemToPublicKeyDER(params.RekorPublicKeyPEM)
	if err != nil {
		return "", fmt.Errorf("decode rekor key: %w", err)
	}

	// For Rekor v2, compute the C2SP checkpoint key ID.
	// Ed25519: SHA-256(origin || "\n" || 0x01 || 32-byte-raw-pubkey)[:4]
	checkpointKeyID, err := computeCheckpointKeyID(params.RekorOrigin, params.RekorPublicKeyPEM)
	if err != nil {
		return "", fmt.Errorf("compute checkpoint key ID: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	keyDetails, err := rekorKeyDetails(params.RekorPublicKeyPEM)
	if err != nil {
		return "", fmt.Errorf("rekor key details: %w", err)
	}

	trustedRoot := map[string]any{
		"mediaType": "application/vnd.dev.sigstore.trustedroot+json;version=0.1",
		"certificateAuthorities": []map[string]any{
			{
				"subject": map[string]any{
					"organization": "OCM Integration Test",
					"commonName":   "test-fulcio-root",
				},
				"uri": "http://fulcio-server:5555",
				"certChain": map[string]any{
					"certificates": []map[string]any{
						{
							"rawBytes": base64.StdEncoding.EncodeToString(fulcioCertDER),
						},
					},
				},
				"validFor": map[string]any{
					"start": now,
				},
			},
		},
		"tlogs": []map[string]any{
			{
				"baseUrl":       params.RekorBaseURL,
				"hashAlgorithm": "SHA2_256",
				"publicKey": map[string]any{
					"rawBytes":   base64.StdEncoding.EncodeToString(rekorKeyDER),
					"keyDetails": keyDetails,
					"validFor": map[string]any{
						"start": now,
					},
				},
				"logId": map[string]any{
					"keyId": base64.StdEncoding.EncodeToString(checkpointKeyID),
				},
			},
		},
		"ctlogs":               buildCTLogEntries(params, now),
		"timestampAuthorities": buildTSAEntries(params, now),
	}

	data, err := json.MarshalIndent(trustedRoot, "", "  ")
	if err != nil {
		return "", err
	}

	outPath := filepath.Join(tmpDir, "trusted_root.json")
	if err := os.WriteFile(outPath, data, 0o600); err != nil {
		return "", err
	}

	return outPath, nil
}

func buildCTLogEntries(params TrustedRootParams, now string) []any {
	if params.CTLogPublicKeyDER == nil {
		return []any{}
	}
	return []any{
		map[string]any{
			"baseUrl":       "http://tesseract:6962",
			"hashAlgorithm": "SHA2_256",
			"publicKey": map[string]any{
				"rawBytes":   base64.StdEncoding.EncodeToString(params.CTLogPublicKeyDER),
				"keyDetails": "PKIX_ECDSA_P256_SHA_256",
				"validFor": map[string]any{
					"start": now,
				},
			},
			"logId": map[string]any{
				"keyId": base64.StdEncoding.EncodeToString(params.CTLogID[:]),
			},
		},
	}
}

func buildTSAEntries(params TrustedRootParams, now string) []any {
	if params.TSACertChainPEM == nil {
		return []any{}
	}

	certs := parsePEMCertificates(params.TSACertChainPEM)
	if len(certs) == 0 {
		return []any{}
	}

	certEntries := make([]map[string]any, 0, len(certs))
	for _, certDER := range certs {
		certEntries = append(certEntries, map[string]any{
			"rawBytes": base64.StdEncoding.EncodeToString(certDER),
		})
	}

	return []any{
		map[string]any{
			"subject": map[string]any{
				"organization": "sigstore.dev",
				"commonName":   "integration-test-tsa",
			},
			"certChain": map[string]any{
				"certificates": certEntries,
			},
			"validFor": map[string]any{
				"start": now,
			},
		},
	}
}

// parsePEMCertificates extracts DER-encoded certificates from a PEM bundle.
func parsePEMCertificates(pemData []byte) [][]byte {
	var certs [][]byte
	rest := pemData
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			certs = append(certs, block.Bytes)
		}
	}
	return certs
}

func pemToDER(pemData []byte, expectedType string) ([]byte, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block of type %s", expectedType)
	}
	if block.Type != expectedType {
		return nil, fmt.Errorf("expected PEM type %s, got %s", expectedType, block.Type)
	}
	return block.Bytes, nil
}

func pemToPublicKeyDER(pemData []byte) ([]byte, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode Rekor public key PEM")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse Rekor public key: %w", err)
	}

	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, err
	}

	return der, nil
}

func rekorKeyDetails(pemData []byte) (string, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", err
	}

	switch k := pub.(type) {
	case *ecdsa.PublicKey:
		switch k.Curve {
		case elliptic.P256():
			return "PKIX_ECDSA_P256_SHA_256", nil
		case elliptic.P384():
			return "PKIX_ECDSA_P384_SHA_384", nil
		case elliptic.P521():
			return "PKIX_ECDSA_P521_SHA_512", nil
		default:
			return "", fmt.Errorf("unsupported ECDSA curve: %v", k.Curve.Params().Name)
		}
	case ed25519.PublicKey:
		return "PKIX_ED25519", nil
	case *rsa.PublicKey:
		return "PKIX_RSA_PKCS1V15_2048_SHA256", nil
	default:
		return "", fmt.Errorf("unsupported Rekor public key type: %T", pub)
	}
}

// computeCheckpointKeyID computes the C2SP signed-note key ID for a Rekor v2 log.
// For Ed25519: SHA-256(origin || "\n" || 0x01 || 32-byte-raw-ed25519-pubkey)[:4]
// See https://github.com/C2SP/C2SP/blob/main/signed-note.md#signatures
//
// NOTE: Only Ed25519 is supported because the integration-test Rekor instance
// uses an Ed25519 signing key (see StartRekor in rekor.go). The C2SP spec
// defines different algorithm prefixes for ECDSA (0x02) and other key types,
// but implementing those is unnecessary until the test infrastructure changes.
func computeCheckpointKeyID(origin string, pubKeyPEM []byte) ([]byte, error) {
	block, _ := pem.Decode(pubKeyPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}

	edKey, ok := pub.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("only Ed25519 keys are supported for checkpoint key ID computation, got %T", pub)
	}

	// C2SP signed-note key ID for Ed25519:
	// SHA-256(key_name || 0x0A || 0x01 || 32-byte-raw-key)
	// key_name = origin string
	h := sha256.New()
	h.Write([]byte(origin))
	h.Write([]byte{0x0A})  // newline
	h.Write([]byte{0x01})  // Ed25519 algorithm identifier
	h.Write([]byte(edKey)) // raw 32-byte public key
	sum := h.Sum(nil)
	return sum[:4], nil
}
