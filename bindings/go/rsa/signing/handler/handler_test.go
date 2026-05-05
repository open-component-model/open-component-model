package handler

import (
	"crypto"
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
	internalpem "ocm.software/open-component-model/bindings/go/rsa/signing/handler/internal/pem"
	rsacredentialsv1 "ocm.software/open-component-model/bindings/go/rsa/spec/credentials/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/rsa/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
)

func Test_RSA_Handler(t *testing.T) {
	// signer A
	aKey := mustKey(t)
	aCert := mustSelfSigned(t, "signer", aKey)
	aPriv, aPub := writeKeyAndChain(t, t.TempDir(), aKey, aCert)

	// signer B (mismatch)
	bKey := mustKey(t)
	bCert := mustSelfSigned(t, "other", bKey)
	_, bPub := writeKeyAndChain(t, t.TempDir(), bKey, bCert)

	h, err := New(v1alpha1.Scheme, false)
	require.NoError(t, err)

	testData := []byte("hello world")

	// test both signature schemes and both hashes
	for _, hashCfg := range []crypto.Hash{
		crypto.SHA256,
		crypto.SHA512,
	} {
		t.Run(hashCfg.String(), func(t *testing.T) {
			for _, alg := range []v1alpha1.SignatureAlgorithm{
				v1alpha1.AlgorithmRSASSAPSS,
				v1alpha1.AlgorithmRSASSAPKCS1V15,
			} {
				d := digestHex(hashCfg, testData)

				t.Run(string(alg), func(t *testing.T) {
					// used for a dynamic root
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
							rsacredentialsv1.CredentialKeyPrivateKeyPEMFile: privPath,
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
							rsacredentialsv1.CredentialKeyPrivateKeyPEMFile: privPath,
							rsacredentialsv1.CredentialKeyPublicKeyPEMFile:  pubPath, // embeds chain
						})
						require.NoError(t, err)
						return descruntime.Signature{Digest: d, Signature: si}
					}

					// buildLeafOnlyPEM creates a PEM signature for a fresh chain instance,
					// embedding only the leaf cert. Used by tests that exercise credential
					// chains independently from the signing chain.
					buildLeafOnlyPEM := func(t *testing.T) descruntime.Signature {
						t.Helper()
						c := buildChain(t)
						dir := t.TempDir()
						privPath := filepath.Join(dir, "leaf.key")
						writePEMFile(t, privPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(c.leafKey))
						leafOnly := writeCertsPEM(t, dir, "leaf.pem", c.leaf)
						return signPEM(t, privPath, leafOnly)
					}

					tests := []tc{
						{
							name:  "plain_hex_signature_with_matching_pub",
							build: func(t *testing.T) descruntime.Signature { return signPlain(t, aPriv) },
							creds: func(t *testing.T) map[string]string {
								return map[string]string{rsacredentialsv1.CredentialKeyPublicKeyPEMFile: aPub}
							},
						},
						{
							name:  "plain_hex_signature_with_matching_pkix_public_key",
							build: func(t *testing.T) descruntime.Signature { return signPlain(t, aPriv) },
							creds: func(t *testing.T) map[string]string {
								p := writePKIXPublicKeyPEM(t, t.TempDir(), &aKey.PublicKey)
								return map[string]string{rsacredentialsv1.CredentialKeyPublicKeyPEMFile: p}
							},
						},
						{
							name:  "plain_hex_signature_with_matching_pkcs1_public_key",
							build: func(t *testing.T) descruntime.Signature { return signPlain(t, aPriv) },
							creds: func(t *testing.T) map[string]string {
								p := writePKCS1PublicKeyPEM(t, t.TempDir(), &aKey.PublicKey)
								return map[string]string{rsacredentialsv1.CredentialKeyPublicKeyPEMFile: p}
							},
						},
						{
							name:  "plain_hex_signature_with_only_priv",
							build: func(t *testing.T) descruntime.Signature { return signPlain(t, aPriv) },
							creds: func(t *testing.T) map[string]string {
								return map[string]string{rsacredentialsv1.CredentialKeyPrivateKeyPEMFile: aPriv}
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
									rsacredentialsv1.CredentialKeyPrivateKeyPEMFile: pkcs8Path,
								})
								require.NoError(t, err)
								return descruntime.Signature{Digest: d, Signature: si}
							},
							creds: func(t *testing.T) map[string]string {
								return map[string]string{rsacredentialsv1.CredentialKeyPublicKeyPEMFile: aPub}
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
								return map[string]string{rsacredentialsv1.CredentialKeyPublicKeyPEMFile: aPub}
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
								return map[string]string{rsacredentialsv1.CredentialKeyPublicKeyPEMFile: aPub}
							},
							wantErr: "issuer mismatch between \"CN=mismatch\" and \"CN=signer\"",
						},
						{
							name:  "pem_signature_with_mismatched_credentials_pub_fails",
							build: func(t *testing.T) descruntime.Signature { return signPEM(t, aPriv, aPub) },
							creds: func(t *testing.T) map[string]string {
								return map[string]string{rsacredentialsv1.CredentialKeyPublicKeyPEMFile: bPub}
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

								embedded := writeCertsPEM(t, dir, "embedded.pem", c.leaf, c.interm)

								cfg := v1alpha1.Config{
									SignatureAlgorithm:      alg,
									SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPEM,
								}
								si, err := h.Sign(t.Context(), d, &cfg, map[string]string{
									rsacredentialsv1.CredentialKeyPrivateKeyPEMFile: privPath,
									rsacredentialsv1.CredentialKeyPublicKeyPEMFile:  embedded,
								})
								require.NoError(t, err)

								rootDir := t.TempDir()
								rootPEM = writeCertsPEM(t, rootDir, "root.pem", c.root)

								// Issuer must match the leaf's X.509 Issuer field, which is the
								// intermediate's Subject (the cert that directly signed the leaf).
								return descruntime.Signature{
									Digest: d,
									Signature: descruntime.SignatureInfo{
										Algorithm: si.Algorithm,
										MediaType: si.MediaType,
										Value:     si.Value,
										Issuer:    c.interm.Subject.String(),
									},
								}
							},
							creds: func(t *testing.T) map[string]string {
								return map[string]string{
									rsacredentialsv1.CredentialKeyPublicKeyPEMFile: rootPEM,
								}
							},
						},
						{
							// A signer must not embed a self-signed root CA in the signature —
							// root trust must come from the verifier's credentials only.
							name: "pem_signature_embedded_root_rejected",
							build: func(t *testing.T) descruntime.Signature {
								c := buildChain(t)

								dir := t.TempDir()
								privPath := filepath.Join(dir, "leaf.key")
								writePEMFile(t, privPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(c.leafKey))

								// Embed full chain including the self-signed root.
								embedded := writeCertsPEM(t, dir, "embedded.pem", c.leaf, c.interm, c.root)

								cfg := v1alpha1.Config{
									SignatureAlgorithm:      alg,
									SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPEM,
								}
								si, err := h.Sign(t.Context(), d, &cfg, map[string]string{
									rsacredentialsv1.CredentialKeyPrivateKeyPEMFile: privPath,
									rsacredentialsv1.CredentialKeyPublicKeyPEMFile:  embedded,
								})
								require.NoError(t, err)

								return descruntime.Signature{Digest: d, Signature: si}
							},
							creds:   func(t *testing.T) map[string]string { return nil },
							wantErr: "must not be embedded in the signature",
						},
						{
							// The signature Issuer field must match the X.509 Issuer field of the
							// leaf certificate, which is the Subject of the CA that directly signed
							// it (the intermediate). Setting it correctly must succeed.
							name: "pem_signature_issuer_matches_leaf_issuer_field",
							build: func(t *testing.T) descruntime.Signature {
								c := buildChain(t)

								dir := t.TempDir()
								privPath := filepath.Join(dir, "leaf.key")
								writePEMFile(t, privPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(c.leafKey))
								// Root is NOT embedded — it will come from credentials.
								embedded := writeCertsPEM(t, dir, "embedded.pem", c.leaf, c.interm)

								cfg := v1alpha1.Config{
									SignatureAlgorithm:      alg,
									SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPEM,
								}
								si, err := h.Sign(t.Context(), d, &cfg, map[string]string{
									rsacredentialsv1.CredentialKeyPrivateKeyPEMFile: privPath,
									rsacredentialsv1.CredentialKeyPublicKeyPEMFile:  embedded,
								})
								require.NoError(t, err)

								rootPEM = writeCertsPEM(t, t.TempDir(), "root.pem", c.root)

								// leaf.Issuer == interm.Subject: set Issuer to that, should succeed.
								return descruntime.Signature{
									Digest: d,
									Signature: descruntime.SignatureInfo{
										Algorithm: si.Algorithm,
										MediaType: si.MediaType,
										Value:     si.Value,
										Issuer:    c.interm.Subject.String(),
									},
								}
							},
							creds: func(t *testing.T) map[string]string {
								return map[string]string{rsacredentialsv1.CredentialKeyPublicKeyPEMFile: rootPEM}
							},
						},
						{
							// Setting the signature Issuer field to a DN that does not match the
							// leaf's X.509 Issuer (e.g. the root's Subject instead of the
							// intermediate's Subject) must be rejected.
							name: "pem_signature_issuer_mismatch_with_leaf_issuer_field",
							build: func(t *testing.T) descruntime.Signature {
								c := buildChain(t)

								dir := t.TempDir()
								privPath := filepath.Join(dir, "leaf.key")
								writePEMFile(t, privPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(c.leafKey))
								// Root is NOT embedded — it will come from credentials.
								embedded := writeCertsPEM(t, dir, "embedded.pem", c.leaf, c.interm)

								cfg := v1alpha1.Config{
									SignatureAlgorithm:      alg,
									SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPEM,
								}
								si, err := h.Sign(t.Context(), d, &cfg, map[string]string{
									rsacredentialsv1.CredentialKeyPrivateKeyPEMFile: privPath,
									rsacredentialsv1.CredentialKeyPublicKeyPEMFile:  embedded,
								})
								require.NoError(t, err)

								rootPEM = writeCertsPEM(t, t.TempDir(), "root.pem", c.root)

								// Set Issuer to root's subject, but leaf was signed by intermediate — mismatch.
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
								return map[string]string{rsacredentialsv1.CredentialKeyPublicKeyPEMFile: rootPEM}
							},
							wantErr: "issuer mismatch",
						},
						{
							// an intermediate CA embedded in the chain
							// must NOT be trusted as a root anchor. Without a root in the chain
							// or credentials, verification must fail.
							name: "pem_signature_intermediate_not_trusted_as_root",
							build: func(t *testing.T) descruntime.Signature {
								c := buildChain(t)
								dir := t.TempDir()
								privPath := filepath.Join(dir, "leaf.key")
								writePEMFile(t, privPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(c.leafKey))
								// Embed leaf + intermediate only — no root.
								embedded := writeCertsPEM(t, dir, "embedded.pem", c.leaf, c.interm)
								return signPEM(t, privPath, embedded)
							},
							// No credentials anchor either — interm must not be elevated to root.
							creds:   func(t *testing.T) map[string]string { return nil },
							wantErr: "certificate signed by unknown authority",
						},
						{
							// with a credentials anchor (root cert), the
							// signature Issuer field must match leaf.Issuer (= intermediate's
							// Subject), NOT the anchor's Subject. Setting Issuer to the root's
							// Subject must now be rejected.
							name: "pem_signature_issuer_set_to_anchor_subject_fails",
							build: func(t *testing.T) descruntime.Signature {
								c := buildChain(t)

								dir := t.TempDir()
								privPath := filepath.Join(dir, "leaf.key")
								writePEMFile(t, privPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(c.leafKey))
								embedded := writeCertsPEM(t, dir, "embedded.pem", c.leaf, c.interm)

								cfg := v1alpha1.Config{
									SignatureAlgorithm:      alg,
									SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPEM,
								}
								si, err := h.Sign(t.Context(), d, &cfg, map[string]string{
									rsacredentialsv1.CredentialKeyPrivateKeyPEMFile: privPath,
									rsacredentialsv1.CredentialKeyPublicKeyPEMFile:  embedded,
								})
								require.NoError(t, err)

								rootDir := t.TempDir()
								rootPEM = writeCertsPEM(t, rootDir, "root.pem", c.root)

								// It must now fail because leaf.Issuer is
								// interm.Subject, not root.Subject.
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
									rsacredentialsv1.CredentialKeyPublicKeyPEMFile: rootPEM,
								}
							},
							wantErr: "issuer mismatch",
						},
						{
							// build and creds use independent buildChain() calls, producing
							// certificates with different keys. The leaf in the signature was
							// signed by a different intermediate than the one in credentials,
							// so verification must fail even though the credential PEM contains
							// both an intermediate and a self-signed root.
							name: "pem_signature_leaf_only_cred_chain_mismatched_instances_fails",
							build: func(t *testing.T) descruntime.Signature {
								c := buildChain(t)
								dir := t.TempDir()
								privPath := filepath.Join(dir, "leaf.key")
								writePEMFile(t, privPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(c.leafKey))
								leafOnly := writeCertsPEM(t, dir, "leaf.pem", c.leaf)
								return signPEM(t, privPath, leafOnly)
							},
							creds: func(t *testing.T) map[string]string {
								c := buildChain(t) // different chain — interm/root don't match the leaf above
								chainPath := writeCertsPEM(t, t.TempDir(), "chain.pem", c.interm, c.root)
								return map[string]string{rsacredentialsv1.CredentialKeyPublicKeyPEMFile: chainPath}
							},
							wantErr: "certificate signed by unknown authority",
						},
						{
							// Credentials contain a multi-cert PEM [interm, root] from the same
							// chain as the signature. The handler routes the intermediate to the
							// intermediates pool and the self-signed root to the roots pool, so
							// the full path leaf→interm→root can be verified successfully.
							name: "pem_signature_leaf_only_cred_chain_interm_and_root_ok",
							build: func(t *testing.T) descruntime.Signature {
								// Share the chain with the creds closure via a captured variable.
								// Both closures run before verification, so we build once here and
								// write the credential path to a shared local var that creds reads.
								c := buildChain(t)
								dir := t.TempDir()
								privPath := filepath.Join(dir, "leaf.key")
								writePEMFile(t, privPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(c.leafKey))
								leafOnly := writeCertsPEM(t, dir, "leaf.pem", c.leaf)

								cfg := v1alpha1.Config{
									SignatureAlgorithm:      alg,
									SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPEM,
								}
								si, err := h.Sign(t.Context(), d, &cfg, map[string]string{
									rsacredentialsv1.CredentialKeyPrivateKeyPEMFile: privPath,
									rsacredentialsv1.CredentialKeyPublicKeyPEMFile:  leafOnly,
								})
								require.NoError(t, err)

								rootPEM = writeCertsPEM(t, t.TempDir(), "chain.pem", c.interm, c.root)

								return descruntime.Signature{Digest: d, Signature: si}
							},
							// Credentials provide [interm, root] from the same chain as the
							// signature.
							creds: func(t *testing.T) map[string]string {
								return map[string]string{rsacredentialsv1.CredentialKeyPublicKeyPEMFile: rootPEM}
							},
						},
						{
							// Credentials contain only a non-self-signed intermediate (no root).
							// The intermediate goes to the intermediates pool; no root exists
							// anywhere → verification must fail.
							name:  "pem_signature_cred_chain_interm_only_no_root_fails",
							build: buildLeafOnlyPEM,
							creds: func(t *testing.T) map[string]string {
								c := buildChain(t)
								intermOnly := writeCertsPEM(t, t.TempDir(), "interm.pem", c.interm)
								return map[string]string{rsacredentialsv1.CredentialKeyPublicKeyPEMFile: intermOnly}
							},
							wantErr: "certificate signed by unknown authority",
						},
						{
							// A self-signed cert in credentials must be the last cert in the chain.
							// Placing it before a non-self-signed cert indicates a malformed chain.
							name:  "pem_signature_cred_chain_self_signed_not_last_fails",
							build: buildLeafOnlyPEM,
							creds: func(t *testing.T) map[string]string {
								c := buildChain(t)
								// Deliberately wrong order: [root, interm] — self-signed root is not last.
								badChain := writeCertsPEM(t, t.TempDir(), "bad.pem", c.root, c.interm)
								return map[string]string{rsacredentialsv1.CredentialKeyPublicKeyPEMFile: badChain}
							},
							wantErr: "must be the last certificate in the credential chain",
						},
						{
							// The signature embeds only the leaf cert; credentials supply only
							// the root CA (not the intermediate). Because the intermediate that
							// signed the leaf is absent from both the embedded chain and
							// credentials, the path from leaf to root cannot be built and
							// verification must fail.
							name:  "pem_signature_leaf_only_signature_only_root_in_credentials_fails",
							build: buildLeafOnlyPEM,
							creds: func(t *testing.T) map[string]string {
								c := buildChain(t)
								rootPath := writeCertsPEM(t, t.TempDir(), "root.pem", c.root)
								return map[string]string{rsacredentialsv1.CredentialKeyPublicKeyPEMFile: rootPath}
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
				})
			}
		})
	}
}

// Test_RSA_Credentials_Override_SystemRoots verifies the anchor isolation rules:
//   - Self-signed anchor in credentials: system roots are ignored entirely;
//     the chain must terminate at exactly that anchor.
//   - Non-self-signed cert in credentials: treated as an intermediate for path
//     building, never as a root anchor. Without a self-signed root, verification
//     falls back to system roots; if no system root covers the chain it fails.
func Test_RSA_Credentials_Override_SystemRoots(t *testing.T) {
	// Use a handler that has system roots loaded.
	h, err := New(v1alpha1.Scheme, true)
	if err != nil {
		t.Skipf("cannot load system roots: %v", err)
	}

	key := mustKey(t)
	cert := mustSelfSigned(t, "system-trusted-signer", key)
	dir := t.TempDir()
	privPath, chainPath := writeKeyAndChain(t, dir, key, cert)

	sum := sha256.Sum256([]byte("payload"))
	d := descruntime.Digest{HashAlgorithm: crypto.SHA256.String(), Value: hex.EncodeToString(sum[:])}

	cfg := v1alpha1.Config{
		SignatureAlgorithm:      v1alpha1.AlgorithmRSASSAPSS,
		SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPEM,
	}
	si, err := h.Sign(t.Context(), d, &cfg, map[string]string{
		rsacredentialsv1.CredentialKeyPrivateKeyPEMFile: privPath,
		rsacredentialsv1.CredentialKeyPublicKeyPEMFile:  chainPath,
	})
	require.NoError(t, err)
	sig := descruntime.Signature{Digest: d, Signature: si}

	// Self-signed anchor: correct cert succeeds.
	t.Run("self_signed_anchor_correct_succeeds", func(t *testing.T) {
		err := h.Verify(t.Context(), sig, nil, map[string]string{
			rsacredentialsv1.CredentialKeyPublicKeyPEMFile: chainPath,
		})
		require.NoError(t, err)
	})

	// Self-signed anchor: a different root in credentials must fail even though
	// system roots are loaded — a self-signed anchor isolates the root pool.
	t.Run("self_signed_anchor_wrong_cert_fails_despite_system_roots", func(t *testing.T) {
		otherKey := mustKey(t)
		otherCert := mustSelfSigned(t, "unrelated-root", otherKey)
		otherPath := writeCertsPEM(t, t.TempDir(), "other.pem", otherCert)

		err := h.Verify(t.Context(), sig, nil, map[string]string{
			rsacredentialsv1.CredentialKeyPublicKeyPEMFile: otherPath,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "certificate verification failed")
	})

	// Non-self-signed certs in credentials are intermediates, not anchors.
	// Supplying only an intermediate (without a self-signed root) must not
	// allow the chain to terminate there — it still needs a root in the pool.
	t.Run("non_self_signed_credential_cert_is_intermediate_not_anchor", func(t *testing.T) {
		c := buildChain(t)

		leafDir := t.TempDir()
		leafPrivPath := filepath.Join(leafDir, "leaf.key")
		writePEMFile(t, leafPrivPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(c.leafKey))
		// Embed only the leaf; interm and root come from credentials.
		leafOnly := writeCertsPEM(t, leafDir, "leaf.pem", c.leaf)

		leafSI, err := h.Sign(t.Context(), d, &cfg, map[string]string{
			rsacredentialsv1.CredentialKeyPrivateKeyPEMFile: leafPrivPath,
			rsacredentialsv1.CredentialKeyPublicKeyPEMFile:  leafOnly,
		})
		require.NoError(t, err)
		leafSig := descruntime.Signature{Digest: d, Signature: leafSI}

		// Credentials supply [interm, root]: interm → intermediates pool,
		// root (self-signed, last) → isolated anchor. Must succeed.
		chainPath := writeCertsPEM(t, t.TempDir(), "chain.pem", c.interm, c.root)
		err = h.Verify(t.Context(), leafSig, nil, map[string]string{
			rsacredentialsv1.CredentialKeyPublicKeyPEMFile: chainPath,
		})
		require.NoError(t, err)

		// Credentials supply only [interm] (no self-signed root). The intermediate
		// goes to the intermediates pool; no root exists to terminate the chain.
		// Must fail — a non-self-signed cert is never elevated to an anchor.
		intermPath := writeCertsPEM(t, t.TempDir(), "interm.pem", c.interm)
		err = h.Verify(t.Context(), leafSig, nil, map[string]string{
			rsacredentialsv1.CredentialKeyPublicKeyPEMFile: intermPath,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "certificate verification failed")
	})
}

func Test_RSA_Verify_ErrorPaths_BothAlgs(t *testing.T) {
	for _, alg := range []v1alpha1.SignatureAlgorithm{v1alpha1.AlgorithmRSASSAPSS, v1alpha1.AlgorithmRSASSAPKCS1V15} {
		t.Run(string(alg), func(t *testing.T) {
			h, err := New(v1alpha1.Scheme, false)
			require.NoError(t, err)

			// Keys and certs.
			key := mustKey(t)
			cert := mustSelfSigned(t, "cn=signer", key)
			dir := t.TempDir()
			privPath, chainPath := writeKeyAndChain(t, dir, key, cert)

			// Base digest.
			sum := sha256.Sum256([]byte("payload"))
			d := descruntime.Digest{HashAlgorithm: crypto.SHA256.String(), Value: hex.EncodeToString(sum[:])}

			// Sign a PEM signature that embeds the cert.
			cfgPEM := v1alpha1.Config{
				SignatureAlgorithm:      alg,
				SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPEM,
			}
			si, err := h.Sign(t.Context(), d, &cfgPEM, map[string]string{
				rsacredentialsv1.CredentialKeyPrivateKeyPEMFile: privPath,
				rsacredentialsv1.CredentialKeyPublicKeyPEMFile:  chainPath,
			})
			require.NoError(t, err)

			t.Run("missing public key for plain media", func(t *testing.T) {
				cfgPlain := v1alpha1.Config{
					SignatureAlgorithm:      alg,
					SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPlain,
				}
				plain, err := h.Sign(t.Context(), d, &cfgPlain, map[string]string{
					rsacredentialsv1.CredentialKeyPrivateKeyPEMFile: privPath,
				})
				require.NoError(t, err)

				err = h.Verify(t.Context(), descruntime.Signature{Digest: d, Signature: plain}, nil, nil)
				require.Error(t, err)
				require.Contains(t, err.Error(), "missing public key, required for plain RSA signatures")
			})

			t.Run("missing hash algorithm", func(t *testing.T) {
				s := descruntime.Signature{Digest: descruntime.Digest{HashAlgorithm: "", Value: d.Value}, Signature: si}
				err := h.Verify(t.Context(), s, nil, map[string]string{rsacredentialsv1.CredentialKeyPublicKeyPEMFile: chainPath})
				require.Error(t, err)
				require.Contains(t, err.Error(), "missing hash algorithm")
			})

			t.Run("missing digest value", func(t *testing.T) {
				s := descruntime.Signature{Digest: descruntime.Digest{HashAlgorithm: "sha256", Value: ""}, Signature: si}
				err := h.Verify(t.Context(), s, nil, map[string]string{rsacredentialsv1.CredentialKeyPublicKeyPEMFile: chainPath})
				require.Error(t, err)
				require.Contains(t, err.Error(), "missing digest value")
			})

			t.Run("unsupported hash algorithm", func(t *testing.T) {
				s := descruntime.Signature{Digest: descruntime.Digest{HashAlgorithm: "sha1", Value: d.Value}, Signature: si}
				err := h.Verify(t.Context(), s, nil, map[string]string{rsacredentialsv1.CredentialKeyPublicKeyPEMFile: chainPath})
				require.Error(t, err)
				require.Contains(t, err.Error(), "unsupported hash algorithm")
			})

			t.Run("hash name mapping accepts SHA-256", func(t *testing.T) {
				s := descruntime.Signature{Digest: descruntime.Digest{HashAlgorithm: "SHA-256", Value: d.Value}, Signature: si}
				err := h.Verify(t.Context(), s, nil, map[string]string{rsacredentialsv1.CredentialKeyPublicKeyPEMFile: chainPath})
				require.NoError(t, err)
			})

			t.Run("tampered digest causes verification error", func(t *testing.T) {
				sum2 := sha256.Sum256([]byte("different"))
				d2 := descruntime.Digest{HashAlgorithm: crypto.SHA256.String(), Value: hex.EncodeToString(sum2[:])}
				err := h.Verify(t.Context(), descruntime.Signature{Digest: d2, Signature: si}, nil, map[string]string{
					rsacredentialsv1.CredentialKeyPublicKeyPEMFile: chainPath,
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
				}, nil, map[string]string{rsacredentialsv1.CredentialKeyPublicKeyPEMFile: chainPath})
				require.Error(t, err)
				require.Contains(t, err.Error(), "invalid certificate format (expected \"CERTIFICATE\" PEM block)")
			})

			t.Run("PEM with mismatched Algorithm header", func(t *testing.T) {
				bad := strings.Replace(si.Value, string("Algorithm: "+alg), "Algorithm: ED25519", 1)
				err := h.Verify(t.Context(), descruntime.Signature{
					Digest: d, Signature: descruntime.SignatureInfo{
						Algorithm: si.Algorithm,
						MediaType: si.MediaType,
						Value:     bad,
					},
				}, nil, map[string]string{rsacredentialsv1.CredentialKeyPublicKeyPEMFile: chainPath})
				require.Error(t, err)
				require.Contains(t, err.Error(), "invalid algorithm")
			})

			t.Run("issuer match succeeds when issuer matches leaf cert issuer field", func(t *testing.T) {
				// cert is self-signed so cert.Issuer == cert.Subject; the Issuer field
				// on the signature must match the leaf certificate's X.509 Issuer field.
				s := descruntime.Signature{Digest: d, Signature: si}
				s.Signature.Issuer = cert.Subject.String()
				err := h.Verify(t.Context(), s, nil, map[string]string{
					rsacredentialsv1.CredentialKeyPublicKeyPEMFile: chainPath,
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

func Test_RSA_Identity(t *testing.T) {
	h, err := New(v1alpha1.Scheme, false)
	require.NoError(t, err)

	d := descruntime.Digest{HashAlgorithm: "sha256", Value: "00"} // value irrelevant for identity

	t.Run("GetSigningCredentialConsumerIdentity", func(t *testing.T) {
		for _, alg := range []v1alpha1.SignatureAlgorithm{v1alpha1.AlgorithmRSASSAPSS, v1alpha1.AlgorithmRSASSAPKCS1V15} {
			t.Run(string(alg), func(t *testing.T) {
				cfg := v1alpha1.Config{
					SignatureAlgorithm:      alg,
					SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPlain,
				}
				id, err := h.GetSigningCredentialConsumerIdentity(t.Context(), "sigA", d, &cfg)
				require.NoError(t, err)
				require.Equal(t, string(alg), id[identityv1.IdentityAttributeAlgorithm])
				require.Equal(t, "sigA", id[identityv1.IdentityAttributeSignature])
			})
		}
	})

	t.Run("GetVerifyingCredentialConsumerIdentity", func(t *testing.T) {
		type tc struct {
			name string
			sig  descruntime.Signature
			want string // expected algorithm attribute (may be "")
		}
		tests := []tc{
			{
				name: "plain_pss_algorithm_set",
				sig: descruntime.Signature{
					Name:   "pss-plain",
					Digest: descruntime.Digest{HashAlgorithm: "sha256", Value: "aa"},
					Signature: descruntime.SignatureInfo{
						Algorithm: string(v1alpha1.AlgorithmRSASSAPSS),
						MediaType: v1alpha1.MediaTypePlainRSASSAPSS,
						Value:     "deadbeef",
					},
				},
				want: string(v1alpha1.AlgorithmRSASSAPSS),
			},
			{
				name: "plain_pkcs1_infer_algorithm_from_media_when_empty",
				sig: descruntime.Signature{
					Name:   "pkcs1-plain",
					Digest: descruntime.Digest{HashAlgorithm: "sha256", Value: "bb"},
					Signature: descruntime.SignatureInfo{
						MediaType: v1alpha1.MediaTypePlainRSASSAPKCS1V15,
						Value:     "deadbeef",
					},
				},
				want: string(v1alpha1.AlgorithmRSASSAPKCS1V15),
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				id, err := h.GetVerifyingCredentialConsumerIdentity(t.Context(), tt.sig, nil)
				require.NoError(t, err)
				require.Equal(t, tt.want, id[identityv1.IdentityAttributeAlgorithm])
				require.Equal(t, tt.sig.Name, id[identityv1.IdentityAttributeSignature])
			})
		}
	})

	t.Run("GetVerifyingCredentialConsumerIdentity_PEM_awareness", func(t *testing.T) {
		// helper to build a minimal SIGNATURE PEM for a given algorithm
		newPEM := func(t *testing.T, alg string) string {
			t.Helper()
			cert := mustSelfSigned(t, "cn=signer", mustKey(t))
			// dummy bytes, no chain needed for identity parsing
			return string(internalpem.SignatureBytesToPem(alg, []byte{0x01}, cert))
		}
		tests := []struct {
			name    string
			sig     descruntime.Signature
			wantAlg string
			wantErr string
		}{
			{
				name: "pem_pss_declared_matches",
				sig: descruntime.Signature{
					Name:   "pem-pss",
					Digest: d,
					Signature: descruntime.SignatureInfo{
						Algorithm: string(v1alpha1.AlgorithmRSASSAPSS),
						MediaType: v1alpha1.MediaTypePEM,
						Value:     newPEM(t, string(v1alpha1.AlgorithmRSASSAPSS)),
					},
				},
				wantAlg: string(v1alpha1.AlgorithmRSASSAPSS),
			},
			{
				name: "pem_pkcs1_declared_matches",
				sig: descruntime.Signature{
					Name:   "pem-pkcs1",
					Digest: d,
					Signature: descruntime.SignatureInfo{
						Algorithm: string(v1alpha1.AlgorithmRSASSAPKCS1V15),
						MediaType: v1alpha1.MediaTypePEM,
						Value:     newPEM(t, string(v1alpha1.AlgorithmRSASSAPKCS1V15)),
					},
				},
				wantAlg: string(v1alpha1.AlgorithmRSASSAPKCS1V15),
			},
			{
				name: "pem_declared_empty_uses_pem_alg",
				sig: descruntime.Signature{
					Name:   "pem-empty-declared",
					Digest: d,
					Signature: descruntime.SignatureInfo{
						Algorithm: "",
						MediaType: v1alpha1.MediaTypePEM,
						Value:     newPEM(t, string(v1alpha1.AlgorithmRSASSAPSS)),
					},
				},
				wantAlg: string(v1alpha1.AlgorithmRSASSAPSS),
			},
			{
				name: "pem_declared_mismatch_errors",
				sig: descruntime.Signature{
					Name:   "pem-mismatch",
					Digest: d,
					Signature: descruntime.SignatureInfo{
						Algorithm: string(v1alpha1.AlgorithmRSASSAPSS),
						MediaType: v1alpha1.MediaTypePEM,
						Value:     newPEM(t, string(v1alpha1.AlgorithmRSASSAPKCS1V15)),
					},
				},
				wantErr: "algorithm mismatch",
			},
			{
				name: "pem_invalid_parse_error",
				sig: descruntime.Signature{
					Name:   "pem-invalid",
					Digest: d,
					Signature: descruntime.SignatureInfo{
						Algorithm: string(v1alpha1.AlgorithmRSASSAPSS),
						MediaType: v1alpha1.MediaTypePEM,
						Value:     "-----BEGIN SIGNATURE-----\nnot-pem\n-----END SIGNATURE-----",
					},
				},
				wantErr: "parse pem signature",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				id, err := h.GetVerifyingCredentialConsumerIdentity(t.Context(), tt.sig, nil)
				if tt.wantErr != "" {
					require.Error(t, err)
					require.Contains(t, err.Error(), tt.wantErr)
					return
				}
				require.NoError(t, err)
				require.Equal(t, tt.wantAlg, id[identityv1.IdentityAttributeAlgorithm])
				require.Equal(t, tt.sig.Name, id[identityv1.IdentityAttributeSignature])
			})
		}
	})
}

func digestHex(algorithm crypto.Hash, b []byte) descruntime.Digest {
	h := algorithm.New()
	h.Write(b)
	hashSum := h.Sum(nil)
	return descruntime.Digest{HashAlgorithm: algorithm.String(), Value: hex.EncodeToString(hashSum[:])}
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

func issueCert(t *testing.T, parent *x509.Certificate, parentKey *rsa.PrivateKey, subjectcn string, isCA bool, pub *rsa.PublicKey) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber:          mustRand128(t),
		Subject:               pkix.Name{CommonName: subjectcn},
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
	// Self-sign the root with rootKey so CheckSignatureFrom(root) passes.
	root := issueCert(t, nil, rootKey, "cn=root", true, &rootKey.PublicKey)

	intermKey := mustKey(t)
	interm := issueCert(t, root, rootKey, "cn=intermediate", true, &intermKey.PublicKey)

	leafKey := mustKey(t)
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
	for line := range strings.SplitSeq(pemWithChain, "\n") {
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
