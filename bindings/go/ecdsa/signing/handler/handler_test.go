package handler

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
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
	ecdsacredentials "ocm.software/open-component-model/bindings/go/ecdsa/signing/handler/internal/credentials"
	internalpem "ocm.software/open-component-model/bindings/go/ecdsa/signing/handler/internal/pem"
	"ocm.software/open-component-model/bindings/go/ecdsa/signing/v1alpha1"
)

func Test_ECDSA_Handler(t *testing.T) {
	for _, curveCfg := range []struct {
		curve elliptic.Curve
		alg   v1alpha1.SignatureAlgorithm
	}{
		{elliptic.P256(), v1alpha1.AlgorithmECDSAP256},
		{elliptic.P384(), v1alpha1.AlgorithmECDSAP384},
		{elliptic.P521(), v1alpha1.AlgorithmECDSAP521},
	} {
		curveCfg := curveCfg
		t.Run(string(curveCfg.alg), func(t *testing.T) {
			// signer A
			aKey := mustKey(t, curveCfg.curve)
			aCert := mustSelfSigned(t, "signer", aKey)
			aPriv, aPub := writeKeyAndChain(t, t.TempDir(), aKey, aCert)

			// signer B (mismatch)
			bKey := mustKey(t, curveCfg.curve)
			bCert := mustSelfSigned(t, "other", bKey)
			_, bPub := writeKeyAndChain(t, t.TempDir(), bKey, bCert)

			h, err := New(v1alpha1.Scheme, false)
			require.NoError(t, err)

			testData := []byte("hello world")

			for _, hashCfg := range []crypto.Hash{
				crypto.SHA256,
				crypto.SHA512,
			} {
				hashCfg := hashCfg
				t.Run(hashCfg.String(), func(t *testing.T) {
					alg := curveCfg.alg
					d := digestHex(hashCfg, testData)

					var rootPEM string

					type tc struct {
						name    string
						build   func(t *testing.T) descruntime.Signature
						creds   func(t *testing.T) map[string]string
						wantErr string
					}

					signPlain := func(t *testing.T, privPath string) descruntime.Signature {
						cfg := v1alpha1.Config{
							SignatureAlgorithm:      alg,
							SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPlain,
						}
						si, err := h.Sign(t.Context(), d, &cfg, map[string]string{
							ecdsacredentials.CredentialKeyPrivateKeyPEMFile: privPath,
						})
						require.NoError(t, err)
						return descruntime.Signature{Digest: d, Signature: si}
					}

					signPEM := func(t *testing.T, privPath, pubPath string) descruntime.Signature {
						cfg := v1alpha1.Config{
							SignatureAlgorithm:      alg,
							SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPEM,
						}
						si, err := h.Sign(t.Context(), d, &cfg, map[string]string{
							ecdsacredentials.CredentialKeyPrivateKeyPEMFile: privPath,
							ecdsacredentials.CredentialKeyPublicKeyPEMFile:  pubPath,
						})
						require.NoError(t, err)
						return descruntime.Signature{Digest: d, Signature: si}
					}

					tests := []tc{
						{
							name:  "plain_hex_signature_with_matching_pub",
							build: func(t *testing.T) descruntime.Signature { return signPlain(t, aPriv) },
							creds: func(t *testing.T) map[string]string {
								return map[string]string{ecdsacredentials.CredentialKeyPublicKeyPEMFile: aPub}
							},
						},
						{
							name:  "plain_hex_signature_with_matching_pkix_public_key",
							build: func(t *testing.T) descruntime.Signature { return signPlain(t, aPriv) },
							creds: func(t *testing.T) map[string]string {
								p := writePKIXPublicKeyPEM(t, t.TempDir(), &aKey.PublicKey)
								return map[string]string{ecdsacredentials.CredentialKeyPublicKeyPEMFile: p}
							},
						},
						{
							name:  "plain_hex_signature_with_only_priv",
							build: func(t *testing.T) descruntime.Signature { return signPlain(t, aPriv) },
							creds: func(t *testing.T) map[string]string {
								return map[string]string{ecdsacredentials.CredentialKeyPrivateKeyPEMFile: aPriv}
							},
						},
						{
							name: "plain_hex_signature_with_pkcs8_private_key",
							build: func(t *testing.T) descruntime.Signature {
								dir := t.TempDir()
								pkcs8Path := writePKCS8PrivateKeyPEM(t, dir, aKey)

								cfg := v1alpha1.Config{
									SignatureAlgorithm:      alg,
									SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPlain,
								}
								si, err := h.Sign(t.Context(), d, &cfg, map[string]string{
									ecdsacredentials.CredentialKeyPrivateKeyPEMFile: pkcs8Path,
								})
								require.NoError(t, err)
								return descruntime.Signature{Digest: d, Signature: si}
							},
							creds: func(t *testing.T) map[string]string {
								return map[string]string{ecdsacredentials.CredentialKeyPublicKeyPEMFile: aPub}
							},
						},
						{
							name: "plain_hex_signature_with_sec1_private_key",
							build: func(t *testing.T) descruntime.Signature {
								dir := t.TempDir()
								sec1Path := writeSEC1PrivateKeyPEM(t, dir, aKey)

								cfg := v1alpha1.Config{
									SignatureAlgorithm:      alg,
									SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPlain,
								}
								si, err := h.Sign(t.Context(), d, &cfg, map[string]string{
									ecdsacredentials.CredentialKeyPrivateKeyPEMFile: sec1Path,
								})
								require.NoError(t, err)
								return descruntime.Signature{Digest: d, Signature: si}
							},
							creds: func(t *testing.T) map[string]string {
								return map[string]string{ecdsacredentials.CredentialKeyPublicKeyPEMFile: aPub}
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
								return map[string]string{ecdsacredentials.CredentialKeyPublicKeyPEMFile: aPub}
							},
						},
						{
							name: "pem_signature_with_matching_credentials_pub_issuer_mismatch",
							build: func(t *testing.T) descruntime.Signature {
								s := signPEM(t, aPriv, aPub)
								s.Signature.Issuer = "cn=mismatch"
								return s
							},
							creds: func(t *testing.T) map[string]string {
								return map[string]string{ecdsacredentials.CredentialKeyPublicKeyPEMFile: aPub}
							},
							wantErr: "issuer mismatch between \"CN=mismatch\" and \"CN=signer\"",
						},
						{
							name:  "pem_signature_with_mismatched_credentials_pub_fails",
							build: func(t *testing.T) descruntime.Signature { return signPEM(t, aPriv, aPub) },
							creds: func(t *testing.T) map[string]string {
								return map[string]string{ecdsacredentials.CredentialKeyPublicKeyPEMFile: bPub}
							},
							wantErr: "certificate signed by unknown authority",
						},
						{
							name: "pem_signature_full_chain_in_signature_root_in_credentials_ok",
							build: func(t *testing.T) descruntime.Signature {
								c := buildChain(t, curveCfg.curve)

								dir := t.TempDir()
								privPath := filepath.Join(dir, "leaf.key")
								der, err := x509.MarshalECPrivateKey(c.leafKey)
								require.NoError(t, err)
								writePEMFile(t, privPath, "EC PRIVATE KEY", der)

								embedded := writeCertsPEM(t, dir, "embedded.pem", c.leaf, c.interm)

								cfg := v1alpha1.Config{
									SignatureAlgorithm:      alg,
									SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPEM,
								}
								si, err := h.Sign(t.Context(), d, &cfg, map[string]string{
									ecdsacredentials.CredentialKeyPrivateKeyPEMFile: privPath,
									ecdsacredentials.CredentialKeyPublicKeyPEMFile:  embedded,
								})
								require.NoError(t, err)

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
									ecdsacredentials.CredentialKeyPublicKeyPEMFile: rootPEM,
								}
							},
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
				})
			}
		})
	}
}

func Test_ECDSA_Verify_ErrorPaths(t *testing.T) {
	for _, curveCfg := range []struct {
		curve elliptic.Curve
		alg   v1alpha1.SignatureAlgorithm
	}{
		{elliptic.P256(), v1alpha1.AlgorithmECDSAP256},
		{elliptic.P384(), v1alpha1.AlgorithmECDSAP384},
		{elliptic.P521(), v1alpha1.AlgorithmECDSAP521},
	} {
		curveCfg := curveCfg
		alg := curveCfg.alg
		t.Run(string(alg), func(t *testing.T) {
			h, err := New(v1alpha1.Scheme, false)
			require.NoError(t, err)

			key := mustKey(t, curveCfg.curve)
			cert := mustSelfSigned(t, "cn=signer", key)
			dir := t.TempDir()
			privPath, chainPath := writeKeyAndChain(t, dir, key, cert)

			sum := sha256.Sum256([]byte("payload"))
			d := descruntime.Digest{HashAlgorithm: crypto.SHA256.String(), Value: hex.EncodeToString(sum[:])}

			cfgPEM := v1alpha1.Config{
				SignatureAlgorithm:      alg,
				SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPEM,
			}
			si, err := h.Sign(t.Context(), d, &cfgPEM, map[string]string{
				ecdsacredentials.CredentialKeyPrivateKeyPEMFile: privPath,
				ecdsacredentials.CredentialKeyPublicKeyPEMFile:  chainPath,
			})
			require.NoError(t, err)

			t.Run("missing public key for plain media", func(t *testing.T) {
				cfgPlain := v1alpha1.Config{
					SignatureAlgorithm:      alg,
					SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPlain,
				}
				plain, err := h.Sign(t.Context(), d, &cfgPlain, map[string]string{
					ecdsacredentials.CredentialKeyPrivateKeyPEMFile: privPath,
				})
				require.NoError(t, err)

				err = h.Verify(t.Context(), descruntime.Signature{Digest: d, Signature: plain}, nil, nil)
				require.Error(t, err)
				require.Contains(t, err.Error(), "missing public key, required for plain ECDSA signatures")
			})

			t.Run("missing hash algorithm", func(t *testing.T) {
				s := descruntime.Signature{Digest: descruntime.Digest{HashAlgorithm: "", Value: d.Value}, Signature: si}
				err := h.Verify(t.Context(), s, nil, map[string]string{ecdsacredentials.CredentialKeyPublicKeyPEMFile: chainPath})
				require.Error(t, err)
				require.Contains(t, err.Error(), "missing hash algorithm")
			})

			t.Run("missing digest value", func(t *testing.T) {
				s := descruntime.Signature{Digest: descruntime.Digest{HashAlgorithm: "sha256", Value: ""}, Signature: si}
				err := h.Verify(t.Context(), s, nil, map[string]string{ecdsacredentials.CredentialKeyPublicKeyPEMFile: chainPath})
				require.Error(t, err)
				require.Contains(t, err.Error(), "missing digest value")
			})

			t.Run("unsupported hash algorithm", func(t *testing.T) {
				s := descruntime.Signature{Digest: descruntime.Digest{HashAlgorithm: "sha1", Value: d.Value}, Signature: si}
				err := h.Verify(t.Context(), s, nil, map[string]string{ecdsacredentials.CredentialKeyPublicKeyPEMFile: chainPath})
				require.Error(t, err)
				require.Contains(t, err.Error(), "unsupported hash algorithm")
			})

			t.Run("tampered digest causes verification error", func(t *testing.T) {
				sum2 := sha256.Sum256([]byte("different"))
				d2 := descruntime.Digest{HashAlgorithm: crypto.SHA256.String(), Value: hex.EncodeToString(sum2[:])}
				err := h.Verify(t.Context(), descruntime.Signature{Digest: d2, Signature: si}, nil, map[string]string{
					ecdsacredentials.CredentialKeyPublicKeyPEMFile: chainPath,
				})
				require.Error(t, err)
				require.Contains(t, err.Error(), "verification error")
			})

			t.Run("PEM with no certificate chain", func(t *testing.T) {
				pemOnlySig := stripCertBlocks(si.Value)
				err := h.Verify(t.Context(), descruntime.Signature{
					Digest: d, Signature: descruntime.SignatureInfo{
						Algorithm: si.Algorithm,
						MediaType: si.MediaType,
						Value:     pemOnlySig,
					},
				}, nil, map[string]string{ecdsacredentials.CredentialKeyPublicKeyPEMFile: chainPath})
				require.Error(t, err)
				require.Contains(t, err.Error(), "invalid certificate format (expected \"CERTIFICATE\" PEM block)")
			})

			t.Run("PEM with mismatched Algorithm header", func(t *testing.T) {
				bad := strings.Replace(si.Value, "Algorithm: "+string(alg), "Algorithm: UNKNOWN-ALG", 1)
				err := h.Verify(t.Context(), descruntime.Signature{
					Digest: d, Signature: descruntime.SignatureInfo{
						Algorithm: si.Algorithm,
						MediaType: si.MediaType,
						Value:     bad,
					},
				}, nil, map[string]string{ecdsacredentials.CredentialKeyPublicKeyPEMFile: chainPath})
				require.Error(t, err)
				require.Contains(t, err.Error(), "invalid algorithm")
			})

			t.Run("issuer match succeeds when underlying cert present", func(t *testing.T) {
				s := descruntime.Signature{Digest: d, Signature: si}
				s.Signature.Issuer = cert.Subject.String()
				err := h.Verify(t.Context(), s, nil, map[string]string{
					ecdsacredentials.CredentialKeyPublicKeyPEMFile: chainPath,
				})
				require.NoError(t, err)
			})

			t.Run("unsupported media type", func(t *testing.T) {
				s := descruntime.Signature{
					Digest: d,
					Signature: descruntime.SignatureInfo{
						Algorithm: string(alg),
						MediaType: "application/unknown",
						Value:     "deadbeef",
					},
				}
				err := h.Verify(t.Context(), s, nil, nil)
				require.Error(t, err)
				require.Contains(t, err.Error(), "unsupported media type")
			})
		})
	}
}

func Test_ECDSA_CurveMismatch(t *testing.T) {
	h, err := New(v1alpha1.Scheme, false)
	require.NoError(t, err)

	// Generate P-256 key but try to sign with P-384 algorithm
	key := mustKey(t, elliptic.P256())
	dir := t.TempDir()
	privPath, _ := writeKeyAndChain(t, dir, key, mustSelfSigned(t, "signer", key))

	d := digestHex(crypto.SHA256, []byte("test"))
	cfg := v1alpha1.Config{
		SignatureAlgorithm:      v1alpha1.AlgorithmECDSAP384,
		SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPlain,
	}
	_, err = h.Sign(t.Context(), d, &cfg, map[string]string{
		ecdsacredentials.CredentialKeyPrivateKeyPEMFile: privPath,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not match algorithm")
}

func Test_ECDSA_Identity(t *testing.T) {
	h, err := New(v1alpha1.Scheme, false)
	require.NoError(t, err)

	d := descruntime.Digest{HashAlgorithm: "sha256", Value: "00"}

	t.Run("GetSigningCredentialConsumerIdentity", func(t *testing.T) {
		for _, alg := range []v1alpha1.SignatureAlgorithm{
			v1alpha1.AlgorithmECDSAP256,
			v1alpha1.AlgorithmECDSAP384,
			v1alpha1.AlgorithmECDSAP521,
		} {
			alg := alg
			t.Run(string(alg), func(t *testing.T) {
				cfg := v1alpha1.Config{
					SignatureAlgorithm:      alg,
					SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPlain,
				}
				id, err := h.GetSigningCredentialConsumerIdentity(t.Context(), "sigA", d, &cfg)
				require.NoError(t, err)
				require.Equal(t, string(alg), id[IdentityAttributeAlgorithm])
				require.Equal(t, "sigA", id[IdentityAttributeSignature])
			})
		}
	})

	t.Run("GetVerifyingCredentialConsumerIdentity", func(t *testing.T) {
		type tc struct {
			name string
			sig  descruntime.Signature
			want string
		}
		tests := []tc{
			{
				name: "plain_p256_algorithm_set",
				sig: descruntime.Signature{
					Name:   "p256-plain",
					Digest: descruntime.Digest{HashAlgorithm: "sha256", Value: "aa"},
					Signature: descruntime.SignatureInfo{
						Algorithm: string(v1alpha1.AlgorithmECDSAP256),
						MediaType: v1alpha1.MediaTypePlainECDSAP256,
						Value:     "deadbeef",
					},
				},
				want: string(v1alpha1.AlgorithmECDSAP256),
			},
			{
				name: "plain_p384_infer_algorithm_from_media_when_empty",
				sig: descruntime.Signature{
					Name:   "p384-plain",
					Digest: descruntime.Digest{HashAlgorithm: "sha256", Value: "bb"},
					Signature: descruntime.SignatureInfo{
						MediaType: v1alpha1.MediaTypePlainECDSAP384,
						Value:     "deadbeef",
					},
				},
				want: string(v1alpha1.AlgorithmECDSAP384),
			},
			{
				name: "plain_p521_infer_algorithm_from_media_when_empty",
				sig: descruntime.Signature{
					Name:   "p521-plain",
					Digest: descruntime.Digest{HashAlgorithm: "sha256", Value: "cc"},
					Signature: descruntime.SignatureInfo{
						MediaType: v1alpha1.MediaTypePlainECDSAP521,
						Value:     "deadbeef",
					},
				},
				want: string(v1alpha1.AlgorithmECDSAP521),
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				id, err := h.GetVerifyingCredentialConsumerIdentity(t.Context(), tt.sig, nil)
				require.NoError(t, err)
				require.Equal(t, tt.want, id[IdentityAttributeAlgorithm])
				require.Equal(t, tt.sig.Name, id[IdentityAttributeSignature])
			})
		}
	})

	t.Run("GetVerifyingCredentialConsumerIdentity_PEM_awareness", func(t *testing.T) {
		newPEM := func(t *testing.T, alg string, curve elliptic.Curve) string {
			t.Helper()
			key := mustKey(t, curve)
			cert := mustSelfSigned(t, "cn=signer", key)
			return string(internalpem.SignatureBytesToPem(alg, []byte{0x01}, cert))
		}
		tests := []struct {
			name    string
			sig     descruntime.Signature
			wantAlg string
			wantErr string
		}{
			{
				name: "pem_p256_declared_matches",
				sig: descruntime.Signature{
					Name:   "pem-p256",
					Digest: d,
					Signature: descruntime.SignatureInfo{
						Algorithm: string(v1alpha1.AlgorithmECDSAP256),
						MediaType: v1alpha1.MediaTypePEM,
						Value:     newPEM(t, string(v1alpha1.AlgorithmECDSAP256), elliptic.P256()),
					},
				},
				wantAlg: string(v1alpha1.AlgorithmECDSAP256),
			},
			{
				name: "pem_declared_empty_uses_pem_alg",
				sig: descruntime.Signature{
					Name:   "pem-empty-declared",
					Digest: d,
					Signature: descruntime.SignatureInfo{
						Algorithm: "",
						MediaType: v1alpha1.MediaTypePEM,
						Value:     newPEM(t, string(v1alpha1.AlgorithmECDSAP384), elliptic.P384()),
					},
				},
				wantAlg: string(v1alpha1.AlgorithmECDSAP384),
			},
			{
				name: "pem_declared_mismatch_errors",
				sig: descruntime.Signature{
					Name:   "pem-mismatch",
					Digest: d,
					Signature: descruntime.SignatureInfo{
						Algorithm: string(v1alpha1.AlgorithmECDSAP256),
						MediaType: v1alpha1.MediaTypePEM,
						Value:     newPEM(t, string(v1alpha1.AlgorithmECDSAP384), elliptic.P384()),
					},
				},
				wantErr: "algorithm mismatch",
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				id, err := h.GetVerifyingCredentialConsumerIdentity(t.Context(), tt.sig, nil)
				if tt.wantErr != "" {
					require.Error(t, err)
					require.Contains(t, err.Error(), tt.wantErr)
					return
				}
				require.NoError(t, err)
				require.Equal(t, tt.wantAlg, id[IdentityAttributeAlgorithm])
				require.Equal(t, tt.sig.Name, id[IdentityAttributeSignature])
			})
		}
	})
}

// ---- test helpers ----

func digestHex(algorithm crypto.Hash, b []byte) descruntime.Digest {
	h := algorithm.New()
	h.Write(b)
	hashSum := h.Sum(nil)
	return descruntime.Digest{HashAlgorithm: algorithm.String(), Value: hex.EncodeToString(hashSum[:])}
}

func mustKey(t *testing.T, curve elliptic.Curve) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(curve, rand.Reader)
	require.NoError(t, err)
	return k
}

func mustSelfSigned(t *testing.T, cn string, key *ecdsa.PrivateKey) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber:          mustRand128(t),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
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

func writeKeyAndChain(t *testing.T, dir string, priv *ecdsa.PrivateKey, chain ...*x509.Certificate) (privPath, chainPath string) {
	t.Helper()
	privPath = filepath.Join(dir, "key.pem")
	der, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	writePEMFile(t, privPath, "EC PRIVATE KEY", der)
	chainPath = writeCertsPEM(t, dir, "chain.pem", chain...)
	return
}

func writePKCS8PrivateKeyPEM(t *testing.T, dir string, key *ecdsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	p := filepath.Join(dir, "key_pkcs8.pem")
	writePEMFile(t, p, "PRIVATE KEY", der)
	return p
}

func writeSEC1PrivateKeyPEM(t *testing.T, dir string, key *ecdsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	p := filepath.Join(dir, "key_sec1.pem")
	writePEMFile(t, p, "EC PRIVATE KEY", der)
	return p
}

func writePKIXPublicKeyPEM(t *testing.T, dir string, pub *ecdsa.PublicKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	require.NoError(t, err)
	p := filepath.Join(dir, "pub_pkix.pem")
	writePEMFile(t, p, "PUBLIC KEY", der)
	return p
}

func issueCert(t *testing.T, parent *x509.Certificate, parentKey *ecdsa.PrivateKey, subjectcn string, isCA bool, pub *ecdsa.PublicKey) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber:          mustRand128(t),
		Subject:               pkix.Name{CommonName: subjectcn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(7 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: isCA,
		IsCA:                  isCA,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
	}
	if isCA {
		tmpl.KeyUsage |= x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	}
	if parent == nil {
		parent = tmpl
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub, parentKey)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

type chain struct {
	root, interm, leaf          *x509.Certificate
	rootKey, intermKey, leafKey *ecdsa.PrivateKey
}

func buildChain(t *testing.T, curve elliptic.Curve) chain {
	t.Helper()
	rootKey := mustKey(t, curve)
	root := issueCert(t, nil, rootKey, "cn=root", true, &rootKey.PublicKey)

	intermKey := mustKey(t, curve)
	interm := issueCert(t, root, rootKey, "cn=intermediate", true, &intermKey.PublicKey)

	leafKey := mustKey(t, curve)
	leaf := issueCert(t, interm, intermKey, "cn=leaf", false, &leafKey.PublicKey)

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
