package handler

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	rsacredentials "ocm.software/open-component-model/bindings/go/rsa/signing/handler/internal/credentials"
	"ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
)

func Test_RSASSA_PSS_Handler(t *testing.T) {
	// signer A
	aKey := mustKey(t)
	aCert := mustSelfSigned(t, "CN=signer", aKey)
	aPriv, aPub := writeKeyAndChain(t, t.TempDir(), aKey, aCert)

	// signer B (mismatch)
	bKey := mustKey(t)
	bCert := mustSelfSigned(t, "CN=other", bKey)
	_, bPub := writeKeyAndChain(t, t.TempDir(), bKey, bCert)

	d := digest256Hex([]byte("hello world"))

	h, err := New(false)
	require.NoError(t, err)

	// used for a dynamic root
	var rootPEM string

	type tc struct {
		name    string
		build   func(t *testing.T) descruntime.Signature
		creds   func(t *testing.T) map[string]string
		wantErr string
	}

	signPlain := func(t *testing.T, privPath string) descruntime.Signature {
		cfg := v1alpha1.PSSConfig{SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPlain}
		si, err := h.Sign(t.Context(), d, &cfg, map[string]string{
			rsacredentials.CredentialKeyPrivateKeyPEMFile: privPath,
		})
		require.NoError(t, err)
		return descruntime.Signature{Digest: d, Signature: si}
	}

	signPEM := func(t *testing.T, privPath, pubPath string) descruntime.Signature {
		cfg := v1alpha1.PSSConfig{SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPEM}
		si, err := h.Sign(t.Context(), d, &cfg, map[string]string{
			rsacredentials.CredentialKeyPrivateKeyPEMFile: privPath,
			rsacredentials.CredentialKeyPublicKeyPEMFile:  pubPath, // embeds chain
		})
		require.NoError(t, err)
		return descruntime.Signature{Digest: d, Signature: si}
	}

	tests := []tc{
		{
			name:  "plain_hex_signature_with_matching_pub",
			build: func(t *testing.T) descruntime.Signature { return signPlain(t, aPriv) },
			creds: func(t *testing.T) map[string]string {
				return map[string]string{rsacredentials.CredentialKeyPublicKeyPEMFile: aPub}
			},
		},
		{
			name:  "plain_hex_signature_with_matching_pkix_public_key",
			build: func(t *testing.T) descruntime.Signature { return signPlain(t, aPriv) },
			creds: func(t *testing.T) map[string]string {
				// create a PKIX SubjectPublicKeyInfo PEM that Verify must accept
				p := writePKIXPublicKeyPEM(t, t.TempDir(), &aKey.PublicKey)
				return map[string]string{rsacredentials.CredentialKeyPublicKeyPEMFile: p}
			},
		},
		{
			name:  "plain_hex_signature_with_matching_pkcs1_public_key",
			build: func(t *testing.T) descruntime.Signature { return signPlain(t, aPriv) },
			creds: func(t *testing.T) map[string]string {
				// create a PKCS#1 RSA PUBLIC KEY PEM that Verify must accept
				p := writePKCS1PublicKeyPEM(t, t.TempDir(), &aKey.PublicKey)
				return map[string]string{rsacredentials.CredentialKeyPublicKeyPEMFile: p}
			},
		},
		{
			name:  "plain_hex_signature_with_only_priv",
			build: func(t *testing.T) descruntime.Signature { return signPlain(t, aPriv) },
			creds: func(t *testing.T) map[string]string {
				return map[string]string{rsacredentials.CredentialKeyPrivateKeyPEMFile: aPriv}
			},
		},
		{
			name: "plain_hex_signature_with_pkcs8_private_key",
			build: func(t *testing.T) descruntime.Signature {
				// sign using a PKCS#8-encoded RSA private key
				dir := t.TempDir()
				pkcs8Path := writePKCS8PrivateKeyPEM(t, dir, aKey)

				cfg := v1alpha1.PSSConfig{SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPlain}
				si, err := h.Sign(t.Context(), d, &cfg, map[string]string{
					rsacredentials.CredentialKeyPrivateKeyPEMFile: pkcs8Path,
				})
				require.NoError(t, err)
				return descruntime.Signature{Digest: d, Signature: si}
			},
			creds: func(t *testing.T) map[string]string {
				// verify against the matching public key (cert chain file is fine)
				return map[string]string{rsacredentials.CredentialKeyPublicKeyPEMFile: aPub}
			},
		},
		{
			name:    "pem_signature_extracts_pub_from_signature_no_credentials",
			build:   func(t *testing.T) descruntime.Signature { return signPEM(t, aPriv, aPub) },
			creds:   func(t *testing.T) map[string]string { return nil },
			wantErr: "certificate signed by unknown authority",
		},
		{
			name:  "pem_signature_with_matching_credentials_pub",
			build: func(t *testing.T) descruntime.Signature { return signPEM(t, aPriv, aPub) },
			creds: func(t *testing.T) map[string]string {
				return map[string]string{rsacredentials.CredentialKeyPublicKeyPEMFile: aPub}
			},
		},
		{
			name: "pem_signature_with_matching_credentials_pub_issuer_mismatch",
			build: func(t *testing.T) descruntime.Signature {
				s := signPEM(t, aPriv, aPub)
				s.Signature.Issuer = "mismatch"
				return s
			},
			creds: func(t *testing.T) map[string]string {
				return map[string]string{rsacredentials.CredentialKeyPublicKeyPEMFile: aPub}
			},
			wantErr: "common name \"mismatch\" does not match expected \"CN=signer\"",
		},
		{
			name:  "pem_signature_with_mismatched_credentials_pub_fails",
			build: func(t *testing.T) descruntime.Signature { return signPEM(t, aPriv, aPub) },
			creds: func(t *testing.T) map[string]string {
				return map[string]string{rsacredentials.CredentialKeyPublicKeyPEMFile: bPub}
			},
			wantErr: "certificate signed by unknown authority",
		},
		{
			name: "pem_signature_full_chain_in_signature_root_in_credentials_ok",
			build: func(t *testing.T) descruntime.Signature {
				c := buildChain(t)

				dir := t.TempDir()
				privPath := filepath.Join(dir, "leaf.key")
				writePEMFile(t, privPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(c.leafKey))

				// embed leaf + intermediate
				embedded := writeCertsPEM(t, dir, "embedded.pem", c.leaf, c.interm)

				cfg := v1alpha1.PSSConfig{SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPEM}
				si, err := h.Sign(t.Context(), d, &cfg, map[string]string{
					rsacredentials.CredentialKeyPrivateKeyPEMFile: privPath,
					rsacredentials.CredentialKeyPublicKeyPEMFile:  embedded,
				})
				require.NoError(t, err)

				// persist the EXACT root for creds()
				rootDir := t.TempDir()
				rootPEM = writeCertsPEM(t, rootDir, "root.pem", c.root)

				return descruntime.Signature{
					Digest: d,
					Signature: descruntime.SignatureInfo{
						Algorithm: si.Algorithm,
						MediaType: si.MediaType,
						Value:     si.Value,
						Issuer:    c.root.Subject.String(),
					},
				}
			},
			creds: func(t *testing.T) map[string]string {
				return map[string]string{
					rsacredentials.CredentialKeyPublicKeyPEMFile: rootPEM,
				}
			},
		},
		{
			name: "pem_signature_leaf_only_signature_only_root_in_credentials_fails",
			build: func(t *testing.T) descruntime.Signature {
				c := buildChain(t)
				dir := t.TempDir()
				privPath := filepath.Join(dir, "leaf.key")
				writePEMFile(t, privPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(c.leafKey))
				leafOnly := writeCertsPEM(t, dir, "leaf.pem", c.leaf)
				return signPEM(t, privPath, leafOnly)
			},
			creds: func(t *testing.T) map[string]string {
				c := buildChain(t)
				rootPath := writeCertsPEM(t, t.TempDir(), "root.pem", c.root)
				return map[string]string{rsacredentials.CredentialKeyPublicKeyPEMFile: rootPath}
			},
			wantErr: "certificate signed by unknown authority",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sig := tt.build(t)
			err := h.Verify(t.Context(), sig, nil, tt.creds(t))
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func Test_RSASSA_PSS_Verify_ErrorPaths(t *testing.T) {
	h, err := New(false)
	require.NoError(t, err)

	// Keys and certs.
	key := mustKey(t)
	cert := mustSelfSigned(t, "CN=signer", key)
	dir := t.TempDir()
	privPath, chainPath := writeKeyAndChain(t, dir, key, cert)

	// Base digest.
	sum := sha256.Sum256([]byte("payload"))
	d := descruntime.Digest{HashAlgorithm: "sha-256", Value: hex.EncodeToString(sum[:])}

	// Sign a PEM signature that embeds the cert.
	cfg := v1alpha1.PSSConfig{SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPEM}
	si, err := h.Sign(t.Context(), d, &cfg, map[string]string{
		rsacredentials.CredentialKeyPrivateKeyPEMFile: privPath,
		rsacredentials.CredentialKeyPublicKeyPEMFile:  chainPath,
	})
	require.NoError(t, err)

	t.Run("missing public key for plain media", func(t *testing.T) {
		// Sign plain with no creds for verify.
		cfg := v1alpha1.PSSConfig{SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPlain}
		plain, err := h.Sign(t.Context(), d, &cfg, map[string]string{
			rsacredentials.CredentialKeyPrivateKeyPEMFile: privPath,
		})
		require.NoError(t, err)

		err = h.Verify(t.Context(), descruntime.Signature{Digest: d, Signature: plain}, nil, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "public key required for plain media type")
	})

	t.Run("missing hash algorithm", func(t *testing.T) {
		s := descruntime.Signature{Digest: descruntime.Digest{HashAlgorithm: "", Value: d.Value}, Signature: si}
		err := h.Verify(t.Context(), s, nil, map[string]string{rsacredentials.CredentialKeyPublicKeyPEMFile: chainPath})
		require.Error(t, err)
		require.Contains(t, err.Error(), "missing hash algorithm")
	})

	t.Run("missing digest value", func(t *testing.T) {
		s := descruntime.Signature{Digest: descruntime.Digest{HashAlgorithm: "sha256", Value: ""}, Signature: si}
		err := h.Verify(t.Context(), s, nil, map[string]string{rsacredentials.CredentialKeyPublicKeyPEMFile: chainPath})
		require.Error(t, err)
		require.Contains(t, err.Error(), "missing digest value")
	})

	t.Run("unsupported hash algorithm", func(t *testing.T) {
		s := descruntime.Signature{Digest: descruntime.Digest{HashAlgorithm: "sha1", Value: d.Value}, Signature: si}
		err := h.Verify(t.Context(), s, nil, map[string]string{rsacredentials.CredentialKeyPublicKeyPEMFile: chainPath})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unsupported hash algorithm")
	})

	t.Run("hash name mapping accepts SHA-256", func(t *testing.T) {
		s := descruntime.Signature{Digest: descruntime.Digest{HashAlgorithm: "SHA-256", Value: d.Value}, Signature: si}
		err := h.Verify(t.Context(), s, nil, map[string]string{rsacredentials.CredentialKeyPublicKeyPEMFile: chainPath})
		require.NoError(t, err)
	})

	t.Run("tampered digest causes verification error", func(t *testing.T) {
		sum2 := sha256.Sum256([]byte("different"))
		d2 := descruntime.Digest{HashAlgorithm: "sha256", Value: hex.EncodeToString(sum2[:])}
		err := h.Verify(t.Context(), descruntime.Signature{Digest: d2, Signature: si}, nil, map[string]string{
			rsacredentials.CredentialKeyPublicKeyPEMFile: chainPath,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "verification error")
	})

	t.Run("PEM with no certificate chain", func(t *testing.T) {
		// Remove all CERTIFICATE blocks from the signed PEM.
		pemOnlySig := stripCertBlocks(si.Value)
		err := h.Verify(t.Context(), descruntime.Signature{
			Digest: d, Signature: descruntime.SignatureInfo{
				Algorithm: si.Algorithm,
				MediaType: si.MediaType,
				Value:     pemOnlySig,
			},
		}, nil, map[string]string{rsacredentials.CredentialKeyPublicKeyPEMFile: chainPath})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid certificate format (expected \"CERTIFICATE\" PEM block)")
	})

	t.Run("PEM with mismatched AlgorithmRSASSAPSS header", func(t *testing.T) {
		bad := strings.Replace(si.Value, "AlgorithmRSASSAPSS: "+AlgorithmRSASSAPSS, "AlgorithmRSASSAPSS: ED25519", 1)
		err := h.Verify(t.Context(), descruntime.Signature{
			Digest: d, Signature: descruntime.SignatureInfo{
				Algorithm: si.Algorithm,
				MediaType: si.MediaType,
				Value:     bad,
			},
		}, nil, map[string]string{rsacredentials.CredentialKeyPublicKeyPEMFile: chainPath})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unexpected signature algorithm")
	})

	t.Run("issuer match succeeds when underlying cert present", func(t *testing.T) {
		s := descruntime.Signature{Digest: d, Signature: si}
		s.Signature.Issuer = cert.Subject.String()
		err := h.Verify(t.Context(), s, nil, map[string]string{
			rsacredentials.CredentialKeyPublicKeyPEMFile: chainPath, // makes underlying=*x509.Certificate
		})
		require.NoError(t, err)
	})

	t.Run("unsupported media type", func(t *testing.T) {
		s := descruntime.Signature{
			Digest: d,
			Signature: descruntime.SignatureInfo{
				Algorithm: AlgorithmRSASSAPSS,
				MediaType: "application/unknown",
				Value:     "deadbeef",
			},
		}
		err := h.Verify(t.Context(), s, nil, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "unsupported media type")
	})
}

func digest256Hex(b []byte) descruntime.Digest {
	sum := sha256.Sum256(b)
	return descruntime.Digest{HashAlgorithm: "sha256", Value: hex.EncodeToString(sum[:])}
}

func mustKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return k
}

func mustSelfSigned(t *testing.T, cn string, key *rsa.PrivateKey) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber:          mustRand128(t),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

func mustRand128(t *testing.T) *big.Int {
	t.Helper()
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)
	return n
}

func writePEMFile(t *testing.T, path, typ string, der []byte) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o600))
}

func writeKeyAndChain(t *testing.T, dir string, priv *rsa.PrivateKey, chain ...*x509.Certificate) (privPath, chainPath string) {
	t.Helper()
	privPath = filepath.Join(dir, "key.pem")
	writePEMFile(t, privPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(priv))
	chainPath = writeCertsPEM(t, dir, "chain.pem", chain...)
	return
}

func writePKCS8PrivateKeyPEM(t *testing.T, dir string, key *rsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	p := filepath.Join(dir, "key_pkcs8.pem")
	writePEMFile(t, p, "PRIVATE KEY", der)
	return p
}

func issueCert(t *testing.T, parent *x509.Certificate, parentKey *rsa.PrivateKey, subjectCN string, isCA bool, pub *rsa.PublicKey) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber:          mustRand128(t),
		Subject:               pkix.Name{CommonName: subjectCN},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(7 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: isCA,
		IsCA:                  isCA,
	}
	if isCA {
		tmpl.KeyUsage |= x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	}
	if parent == nil {
		parent = tmpl
	}
	if parentKey == nil {
		parentKey = mustKey(t)
		if pub == nil {
			pub = &parentKey.PublicKey
		}
	}
	if pub == nil {
		priv := mustKey(t)
		pub = &priv.PublicKey
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub, parentKey)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

type chain struct {
	root, interm, leaf          *x509.Certificate
	rootKey, intermKey, leafKey *rsa.PrivateKey
}

func buildChain(t *testing.T) chain {
	t.Helper()
	rootKey := mustKey(t)
	root := issueCert(t, nil, nil, "CN=root", true, &rootKey.PublicKey)

	intermKey := mustKey(t)
	interm := issueCert(t, root, rootKey, "CN=intermediate", true, &intermKey.PublicKey)

	leafKey := mustKey(t)
	leaf := issueCert(t, interm, intermKey, "CN=leaf", false, &leafKey.PublicKey)

	return chain{root, interm, leaf, rootKey, intermKey, leafKey}
}

func writeCertsPEM(t *testing.T, dir, name string, certs ...*x509.Certificate) string {
	t.Helper()
	var blob []byte
	for _, c := range certs {
		blob = append(blob, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})...)
	}
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, blob, 0o600))
	return p
}

func writePKIXPublicKeyPEM(t *testing.T, dir string, pub *rsa.PublicKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	require.NoError(t, err)
	p := filepath.Join(dir, "pub_pkix.pem")
	writePEMFile(t, p, "PUBLIC KEY", der)
	return p
}

func writePKCS1PublicKeyPEM(t *testing.T, dir string, pub *rsa.PublicKey) string {
	t.Helper()
	der := x509.MarshalPKCS1PublicKey(pub)
	p := filepath.Join(dir, "pub_pkcs1.pem")
	writePEMFile(t, p, "RSA PUBLIC KEY", der)
	return p
}

func stripCertBlocks(pemWithChain string) string {
	var out []string
	inCert := false
	for _, line := range strings.Split(pemWithChain, "\n") {
		switch line {
		case "-----BEGIN CERTIFICATE-----":
			inCert = true
			continue
		case "-----END CERTIFICATE-----":
			inCert = false
			continue
		}
		if !inCert {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
