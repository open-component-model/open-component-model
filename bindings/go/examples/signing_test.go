package examples

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	rsahandler "ocm.software/open-component-model/bindings/go/rsa/signing/handler"
	"ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/signing"
)

// TestExample_GenerateDigest demonstrates generating a SHA-256 digest for a
// component descriptor using the v4alpha1 normalisation algorithm.
func TestExample_GenerateDigest(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	desc := &descriptor.Descriptor{
		Component: descriptor.Component{
			Provider: descriptor.Provider{Name: "acme.org"},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "acme.org/my-app",
					Version: "1.0.0",
				},
			},
		},
	}

	dig, err := signing.GenerateDigest(ctx, desc, logger, v4alpha1.Algorithm, crypto.SHA256.String())
	r.NoError(err)
	r.NotNil(dig)

	// The digest contains the algorithms used and the hex-encoded hash value.
	r.Equal(v4alpha1.Algorithm, dig.NormalisationAlgorithm)
	r.Equal(crypto.SHA256.String(), dig.HashAlgorithm)

	// Verify the value is a valid 64-character hex string (32 bytes).
	b, err := hex.DecodeString(dig.Value)
	r.NoError(err)
	r.Len(b, 32)
}

// TestExample_VerifyDigest shows how to generate a digest and then verify it
// against the same descriptor to confirm integrity.
func TestExample_VerifyDigest(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	desc := &descriptor.Descriptor{
		Component: descriptor.Component{
			Provider: descriptor.Provider{Name: "acme.org"},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "acme.org/verified-app",
					Version: "2.0.0",
				},
			},
		},
	}

	// Generate a digest.
	dig, err := signing.GenerateDigest(ctx, desc, logger, v4alpha1.Algorithm, crypto.SHA256.String())
	r.NoError(err)

	// Verify the digest matches the descriptor by embedding it in a signature.
	sig := descriptor.Signature{
		Name:   "test-signature",
		Digest: *dig,
	}
	r.NoError(signing.VerifyDigestMatchesDescriptor(ctx, desc, sig, logger))
}

// TestExample_CheckDigestibility validates whether a component's references and
// resources are safely digestible before attempting digest generation.
func TestExample_CheckDigestibility(t *testing.T) {
	r := require.New(t)

	// A component with a properly digested reference is safe.
	comp := &descriptor.Component{
		References: []descriptor.Reference{
			{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{Name: "ref", Version: "v1"},
				},
				Component: "acme.org/dependency",
				Digest: descriptor.Digest{
					HashAlgorithm:          crypto.SHA256.String(),
					NormalisationAlgorithm: v4alpha1.Algorithm,
					Value:                  "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
				},
			},
		},
	}
	r.NoError(signing.IsSafelyDigestible(comp))

	// A reference without digest fields will fail the check.
	compBad := &descriptor.Component{
		References: []descriptor.Reference{
			{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{Name: "ref", Version: "v1"},
				},
				Component: "acme.org/missing-digest",
				Digest:    descriptor.Digest{},
			},
		},
	}
	r.Error(signing.IsSafelyDigestible(compBad))
}

// --- RSA signing and verification helpers ---

// generateRSAKey creates a fresh 2048-bit RSA key pair for testing.
func generateRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return key
}

// selfSignedCert creates a self-signed CA certificate for the given key.
func selfSignedCert(t *testing.T, cn string, key *rsa.PrivateKey) *x509.Certificate {
	t.Helper()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

// writeKeyPEM writes an RSA private key as a PEM file and returns its path.
func writeKeyPEM(t *testing.T, dir string, key *rsa.PrivateKey) string {
	t.Helper()
	path := filepath.Join(dir, "key.pem")
	data := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

// writeCertPEM writes one or more certificates as a PEM file and returns its path.
func writeCertPEM(t *testing.T, dir, name string, certs ...*x509.Certificate) string {
	t.Helper()
	var data []byte
	for _, c := range certs {
		data = append(data, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})...)
	}
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

// TestExample_RSASignAndVerifyPlain demonstrates the full RSA signing workflow:
// generate a key pair, compute a descriptor digest, sign with the RSA handler
// using plain hex encoding, and verify the signature.
func TestExample_RSASignAndVerifyPlain(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// 1. Generate an RSA key pair and write to temp files.
	key := generateRSAKey(t)
	cert := selfSignedCert(t, "example-signer", key)
	dir := t.TempDir()
	privPath := writeKeyPEM(t, dir, key)
	pubPath := writeCertPEM(t, dir, "cert.pem", cert)

	// 2. Build a component descriptor.
	desc := &descriptor.Descriptor{
		Component: descriptor.Component{
			Provider: descriptor.Provider{Name: "acme.org"},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "acme.org/signed-app",
					Version: "1.0.0",
				},
			},
		},
	}

	// 3. Generate the descriptor digest.
	dig, err := signing.GenerateDigest(ctx, desc, logger, v4alpha1.Algorithm, crypto.SHA256.String())
	r.NoError(err)

	// 4. Create the RSA signing handler and sign.
	handler, err := rsahandler.New(v1alpha1.Scheme, false)
	r.NoError(err)

	cfg := &v1alpha1.Config{
		SignatureAlgorithm:      v1alpha1.AlgorithmRSASSAPSS,
		SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPlain,
	}
	sigInfo, err := handler.Sign(ctx, *dig, cfg, map[string]string{
		"private_key_pem_file": privPath,
	})
	r.NoError(err)
	r.NotEmpty(sigInfo.Value)

	// 5. Assemble the full signature and verify it.
	fullSig := descriptor.Signature{
		Name:      "example-sig",
		Digest:    *dig,
		Signature: sigInfo,
	}
	err = handler.Verify(ctx, fullSig, nil, map[string]string{
		"public_key_pem_file": pubPath,
	})
	r.NoError(err)
}

// TestExample_RSASignAndVerifyPEM demonstrates RSA signing with PEM-encoded
// signatures that embed the certificate chain. Verification extracts the
// public key from the embedded chain and validates it against a trust anchor.
func TestExample_RSASignAndVerifyPEM(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Generate key and self-signed certificate.
	key := generateRSAKey(t)
	cert := selfSignedCert(t, "pem-signer", key)
	dir := t.TempDir()
	privPath := writeKeyPEM(t, dir, key)
	// The public file contains the certificate chain (just the self-signed cert here).
	pubPath := writeCertPEM(t, dir, "chain.pem", cert)

	// Build descriptor and compute digest.
	desc := &descriptor.Descriptor{
		Component: descriptor.Component{
			Provider: descriptor.Provider{Name: "acme.org"},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "acme.org/pem-signed-app",
					Version: "1.0.0",
				},
			},
		},
	}
	dig, err := signing.GenerateDigest(ctx, desc, logger, v4alpha1.Algorithm, crypto.SHA256.String())
	r.NoError(err)

	// Sign with PEM encoding (embeds certificate chain in signature).
	handler, err := rsahandler.New(v1alpha1.Scheme, false)
	r.NoError(err)

	cfg := &v1alpha1.Config{
		SignatureAlgorithm:      v1alpha1.AlgorithmRSASSAPSS,
		SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPEM,
	}
	sigInfo, err := handler.Sign(ctx, *dig, cfg, map[string]string{
		"private_key_pem_file": privPath,
		"public_key_pem_file":  pubPath,
	})
	r.NoError(err)
	r.Equal(v1alpha1.MediaTypePEM, sigInfo.MediaType)

	// Verify: the trust anchor (self-signed cert) is provided via credentials.
	fullSig := descriptor.Signature{
		Name:      "pem-sig",
		Digest:    *dig,
		Signature: sigInfo,
	}
	err = handler.Verify(ctx, fullSig, nil, map[string]string{
		"public_key_pem_file": pubPath,
	})
	r.NoError(err)
}

// TestExample_RSAVerifyTamperedDigest shows that verification fails when the
// descriptor is modified after signing.
func TestExample_RSAVerifyTamperedDigest(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	key := generateRSAKey(t)
	cert := selfSignedCert(t, "tamper-signer", key)
	dir := t.TempDir()
	privPath := writeKeyPEM(t, dir, key)
	pubPath := writeCertPEM(t, dir, "cert.pem", cert)

	// Sign the original descriptor.
	desc := &descriptor.Descriptor{
		Component: descriptor.Component{
			Provider: descriptor.Provider{Name: "acme.org"},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "acme.org/tamper-test",
					Version: "1.0.0",
				},
			},
		},
	}
	dig, err := signing.GenerateDigest(ctx, desc, logger, v4alpha1.Algorithm, crypto.SHA256.String())
	r.NoError(err)

	handler, err := rsahandler.New(v1alpha1.Scheme, false)
	r.NoError(err)

	cfg := &v1alpha1.Config{
		SignatureAlgorithm:      v1alpha1.AlgorithmRSASSAPSS,
		SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPlain,
	}
	sigInfo, err := handler.Sign(ctx, *dig, cfg, map[string]string{
		"private_key_pem_file": privPath,
	})
	r.NoError(err)

	// Compute a different digest (simulating a tampered descriptor).
	tamperedDesc := &descriptor.Descriptor{
		Component: descriptor.Component{
			Provider: descriptor.Provider{Name: "evil.org"},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "acme.org/tamper-test",
					Version: "1.0.0",
				},
			},
		},
	}
	tamperedDig, err := signing.GenerateDigest(ctx, tamperedDesc, logger, v4alpha1.Algorithm, crypto.SHA256.String())
	r.NoError(err)

	// Verification with the tampered digest should fail.
	fullSig := descriptor.Signature{
		Name:      "tampered-sig",
		Digest:    *tamperedDig,
		Signature: sigInfo,
	}
	err = handler.Verify(ctx, fullSig, nil, map[string]string{
		"public_key_pem_file": pubPath,
	})
	r.Error(err)
	r.ErrorContains(err, "verification error")
}

// TestExample_RSAVerifyWrongKey shows that verification fails when a different
// key pair is used for verification than was used for signing.
func TestExample_RSAVerifyWrongKey(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Sign with key A.
	keyA := generateRSAKey(t)
	dirA := t.TempDir()
	privPathA := writeKeyPEM(t, dirA, keyA)

	// Prepare key B for verification.
	keyB := generateRSAKey(t)
	certB := selfSignedCert(t, "wrong-key", keyB)
	dirB := t.TempDir()
	pubPathB := writeCertPEM(t, dirB, "cert.pem", certB)

	desc := &descriptor.Descriptor{
		Component: descriptor.Component{
			Provider: descriptor.Provider{Name: "acme.org"},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "acme.org/wrong-key-test",
					Version: "1.0.0",
				},
			},
		},
	}
	dig, err := signing.GenerateDigest(ctx, desc, logger, v4alpha1.Algorithm, crypto.SHA256.String())
	r.NoError(err)

	handler, err := rsahandler.New(v1alpha1.Scheme, false)
	r.NoError(err)

	cfg := &v1alpha1.Config{
		SignatureAlgorithm:      v1alpha1.AlgorithmRSASSAPSS,
		SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPlain,
	}
	sigInfo, err := handler.Sign(ctx, *dig, cfg, map[string]string{
		"private_key_pem_file": privPathA,
	})
	r.NoError(err)

	// Verify with key B - should fail.
	fullSig := descriptor.Signature{
		Name:      "wrong-key-sig",
		Digest:    *dig,
		Signature: sigInfo,
	}
	err = handler.Verify(ctx, fullSig, nil, map[string]string{
		"public_key_pem_file": pubPathB,
	})
	r.Error(err)
	r.ErrorContains(err, "verification error")
}
