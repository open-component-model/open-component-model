package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"

	"ocm.software/open-component-model/bindings/go/sigstore/integration/internal"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/handler"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/v1alpha1"
)

//nolint:gosec // these are not secrets
const (
	credOIDCToken           = "token"
	credTrustedRootJSONFile = "trusted_root_json_file"
)

// stack is the shared sigstore infrastructure used by all tests.
// It is started once in TestMain and destroyed after all tests complete.
//
// All tests share the same Rekor transparency log. Each test creates its own
// unique digest (via uniqueDigest), so entries from different tests do not
// interfere. Do not run sub-tests in parallel against the same signed value
// without ensuring digest uniqueness.
//
// The OIDC token stored in stack.OIDCToken is also shared. Dex tokens have a
// short TTL (~5–10 min). For the current suite this is fine; see
// internal.StartSigstoreStack for guidance if the suite grows.
var stack *internal.SigstoreStack

func TestMain(m *testing.M) {
	ctx := context.Background()

	var err error
	stack, err = internal.StartSigstoreStack(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start sigstore stack: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	if err := stack.Destroy(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "failed to destroy sigstore stack: %v\n", err)
	}

	os.Exit(code)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func uniqueDigest(t *testing.T, label string) descruntime.Digest {
	t.Helper()
	h := sha256.Sum256([]byte("integration-tc-" + label + "-" + t.Name()))
	return descruntime.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "jsonNormalisation/v2",
		Value:                  hex.EncodeToString(h[:]),
	}
}

type bundleJSON struct {
	MediaType            string `json:"mediaType"`
	VerificationMaterial struct {
		Certificate *struct {
			RawBytes string `json:"rawBytes"`
		} `json:"certificate"`
		TlogEntries               []json.RawMessage `json:"tlogEntries"`
		TimestampVerificationData json.RawMessage   `json:"timestampVerificationData,omitempty"`
	} `json:"verificationMaterial"`
	MessageSignature *struct {
		Signature string `json:"signature"`
	} `json:"messageSignature"`
}

func decodeBundle(t *testing.T, sigInfo descruntime.SignatureInfo) *bundleJSON {
	t.Helper()
	r := require.New(t)
	raw, err := base64.StdEncoding.DecodeString(sigInfo.Value)
	r.NoError(err)
	var b bundleJSON
	r.NoError(json.Unmarshal(raw, &b))
	return &b
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func Test_Integration_Keyless_IdentityVerification(t *testing.T) {
	r := require.New(t)
	h := handler.New()
	digest := uniqueDigest(t, "identity-verify")

	signCfg := &v1alpha1.SignConfig{
		SigningConfig: stack.SigningConfigPath,
		TrustedRoot:   stack.TrustedRootPath,
	}
	signCfg.SetType(runtime.NewVersionedType(v1alpha1.SignConfigType, v1alpha1.Version))

	sigInfo, err := h.Sign(t.Context(), digest, signCfg, map[string]string{
		credOIDCToken: stack.OIDCToken,
	})
	r.NoError(err)
	r.NotEmpty(sigInfo.Issuer)

	signed := descruntime.Signature{
		Name:      "integration-tc-identity-test",
		Digest:    digest,
		Signature: sigInfo,
	}

	t.Run("matching issuer succeeds", func(t *testing.T) {
		r := require.New(t)
		verifyCfg := &v1alpha1.VerifyConfig{
			TrustedRoot:               stack.TrustedRootPath,
			CertificateOIDCIssuer:     sigInfo.Issuer,
			CertificateIdentityRegexp: ".*",
		}
		verifyCfg.SetType(runtime.NewVersionedType(v1alpha1.VerifyConfigType, v1alpha1.Version))

		err := h.Verify(t.Context(), signed, verifyCfg, map[string]string{
			credTrustedRootJSONFile: stack.TrustedRootPath,
		})
		r.NoError(err)
	})

	t.Run("wrong issuer fails", func(t *testing.T) {
		r := require.New(t)
		verifyCfg := &v1alpha1.VerifyConfig{
			TrustedRoot:               stack.TrustedRootPath,
			CertificateOIDCIssuer:     "https://wrong-issuer.example.com",
			CertificateIdentityRegexp: ".*",
		}
		verifyCfg.SetType(runtime.NewVersionedType(v1alpha1.VerifyConfigType, v1alpha1.Version))

		err := h.Verify(t.Context(), signed, verifyCfg, map[string]string{
			credTrustedRootJSONFile: stack.TrustedRootPath,
		})
		r.Error(err)
	})

	t.Run("issuer regex succeeds", func(t *testing.T) {
		r := require.New(t)
		verifyCfg := &v1alpha1.VerifyConfig{
			TrustedRoot:                 stack.TrustedRootPath,
			CertificateOIDCIssuerRegexp: ".*",
			CertificateIdentityRegexp:   ".*",
		}
		verifyCfg.SetType(runtime.NewVersionedType(v1alpha1.VerifyConfigType, v1alpha1.Version))

		err := h.Verify(t.Context(), signed, verifyCfg, map[string]string{
			credTrustedRootJSONFile: stack.TrustedRootPath,
		})
		r.NoError(err)
	})

	t.Run("matching identity succeeds", func(t *testing.T) {
		r := require.New(t)
		verifyCfg := &v1alpha1.VerifyConfig{
			TrustedRoot:                 stack.TrustedRootPath,
			CertificateOIDCIssuerRegexp: ".*",
			CertificateIdentity:         stack.OIDCIdentity,
		}
		verifyCfg.SetType(runtime.NewVersionedType(v1alpha1.VerifyConfigType, v1alpha1.Version))

		err := h.Verify(t.Context(), signed, verifyCfg, map[string]string{
			credTrustedRootJSONFile: stack.TrustedRootPath,
		})
		r.NoError(err)
	})

	t.Run("wrong identity fails", func(t *testing.T) {
		r := require.New(t)
		verifyCfg := &v1alpha1.VerifyConfig{
			TrustedRoot:                 stack.TrustedRootPath,
			CertificateOIDCIssuerRegexp: ".*",
			CertificateIdentity:         "wrong@example.com",
		}
		verifyCfg.SetType(runtime.NewVersionedType(v1alpha1.VerifyConfigType, v1alpha1.Version))

		err := h.Verify(t.Context(), signed, verifyCfg, map[string]string{
			credTrustedRootJSONFile: stack.TrustedRootPath,
		})
		r.Error(err)
	})
}

// Test_Integration_TamperedBundle signs a real digest, then mutates the
// resulting bundle in various ways and asserts that every mutation causes
// verification to fail.  This proves that cosign detects tampered
// signatures / bundles and cannot be fooled by crafted material.
func Test_Integration_TamperedBundle(t *testing.T) {
	r := require.New(t)
	h := handler.New()
	digest := uniqueDigest(t, "tamper")

	signCfg := &v1alpha1.SignConfig{
		SigningConfig: stack.SigningConfigPath,
		TrustedRoot:   stack.TrustedRootPath,
	}
	signCfg.SetType(runtime.NewVersionedType(v1alpha1.SignConfigType, v1alpha1.Version))

	sigInfo, err := h.Sign(t.Context(), digest, signCfg, map[string]string{
		credOIDCToken: stack.OIDCToken,
	})
	r.NoError(err, "baseline signing must succeed")

	verifyCfg := &v1alpha1.VerifyConfig{
		TrustedRoot:           stack.TrustedRootPath,
		CertificateOIDCIssuer: stack.OIDCIssuer,
		CertificateIdentity:   stack.OIDCIdentity,
	}
	verifyCfg.SetType(runtime.NewVersionedType(v1alpha1.VerifyConfigType, v1alpha1.Version))

	creds := map[string]string{credTrustedRootJSONFile: stack.TrustedRootPath}

	// Baseline: the unmodified bundle must verify cleanly.
	signed := descruntime.Signature{
		Name:      "tamper-baseline",
		Digest:    digest,
		Signature: sigInfo,
	}
	r.NoError(h.Verify(t.Context(), signed, verifyCfg, creds), "baseline verification must succeed")

	// mutateBundle decodes the bundle, applies f to the parsed JSON map, and
	// re-encodes it as a base64 string so it can be placed back into a
	// SignatureInfo.Value.
	mutateBundle := func(t *testing.T, r *require.Assertions, f func(m map[string]any)) string {
		t.Helper()
		raw, err := base64.StdEncoding.DecodeString(sigInfo.Value)
		r.NoError(err)
		var m map[string]any
		r.NoError(json.Unmarshal(raw, &m))
		f(m)
		modified, err := json.Marshal(m)
		r.NoError(err)
		return base64.StdEncoding.EncodeToString(modified)
	}

	t.Run("mutated signature bytes rejected", func(t *testing.T) {
		r := require.New(t)
		// Decode the real signature, flip the last byte, re-encode.
		b := decodeBundle(t, sigInfo)
		r.NotNil(b.MessageSignature, "bundle must have message signature")
		sigBytes, err := base64.StdEncoding.DecodeString(b.MessageSignature.Signature)
		r.NoError(err)
		sigBytes[len(sigBytes)-1] ^= 0xFF // flip all bits in last byte

		tampered := mutateBundle(t, r, func(m map[string]any) {
			vm := m["verificationMaterial"].(map[string]any)
			_ = vm // not needed; mutate messageSignature directly
			ms := m["messageSignature"].(map[string]any)
			ms["signature"] = base64.StdEncoding.EncodeToString(sigBytes)
		})

		s := descruntime.Signature{
			Name:   "tamper-sig-bytes",
			Digest: digest,
			Signature: descruntime.SignatureInfo{
				Algorithm: sigInfo.Algorithm,
				MediaType: sigInfo.MediaType,
				Value:     tampered,
				Issuer:    sigInfo.Issuer,
			},
		}
		err = h.Verify(t.Context(), s, verifyCfg, creds)
		r.Error(err, "verification must fail when signature bytes are mutated")
	})

	t.Run("stripped certificate rejected", func(t *testing.T) {
		r := require.New(t)
		tampered := mutateBundle(t, r, func(m map[string]any) {
			vm := m["verificationMaterial"].(map[string]any)
			delete(vm, "certificate")
		})

		s := descruntime.Signature{
			Name:   "tamper-strip-cert",
			Digest: digest,
			Signature: descruntime.SignatureInfo{
				Algorithm: sigInfo.Algorithm,
				MediaType: sigInfo.MediaType,
				Value:     tampered,
				Issuer:    sigInfo.Issuer,
			},
		}
		err := h.Verify(t.Context(), s, verifyCfg, creds)
		r.Error(err, "verification must fail when certificate is stripped from bundle")
	})

	t.Run("stripped tlog entries rejected", func(t *testing.T) {
		r := require.New(t)
		tampered := mutateBundle(t, r, func(m map[string]any) {
			vm := m["verificationMaterial"].(map[string]any)
			vm["tlogEntries"] = []any{}
		})

		s := descruntime.Signature{
			Name:   "tamper-strip-tlog",
			Digest: digest,
			Signature: descruntime.SignatureInfo{
				Algorithm: sigInfo.Algorithm,
				MediaType: sigInfo.MediaType,
				Value:     tampered,
				Issuer:    sigInfo.Issuer,
			},
		}
		err := h.Verify(t.Context(), s, verifyCfg, creds)
		r.Error(err, "verification must fail when tlog entries are stripped from bundle")
	})

	t.Run("wrong digest rejected", func(t *testing.T) {
		r := require.New(t)
		// Use the genuine bundle but present a different digest for verification.
		wrongDigest := uniqueDigest(t, "tamper-wrong-digest-other")
		s := descruntime.Signature{
			Name:      "tamper-wrong-digest",
			Digest:    wrongDigest, // different from what was signed
			Signature: sigInfo,
		}
		err := h.Verify(t.Context(), s, verifyCfg, creds)
		r.Error(err, "verification must fail when digest does not match signed content")
	})

	t.Run("corrupted bundle rejected", func(t *testing.T) {
		r := require.New(t)
		// Replace the bundle value with garbage that is still valid base64 but
		// not a valid Sigstore bundle JSON.
		garbage := base64.StdEncoding.EncodeToString([]byte(`{"not":"a valid bundle"}`))
		s := descruntime.Signature{
			Name:   "tamper-corrupt-bundle",
			Digest: digest,
			Signature: descruntime.SignatureInfo{
				Algorithm: sigInfo.Algorithm,
				MediaType: sigInfo.MediaType,
				Value:     garbage,
				Issuer:    sigInfo.Issuer,
			},
		}
		err := h.Verify(t.Context(), s, verifyCfg, creds)
		r.Error(err, "verification must fail for a corrupted/garbage bundle")
	})
}

// Test_Integration_OfflineVerification signs with the full sigstore stack
// (Fulcio + TesseraCT + Rekor v2 + TSA), then verifies using only the bundle
// and trusted root — no service URLs, no PrivateInfrastructure flag. This
// exercises the air-gapped verification path where cosign validates tlog entries
// and signed timestamps locally against the trusted root.
func Test_Integration_OfflineVerification(t *testing.T) {
	r := require.New(t)
	h := handler.New()
	digest := uniqueDigest(t, "offline-verify")

	// --- Sign with full stack (needs network to reach Fulcio + Rekor + TSA) ---

	signCfg := &v1alpha1.SignConfig{
		SigningConfig: stack.SigningConfigPath,
		TrustedRoot:   stack.TrustedRootPath,
	}
	signCfg.SetType(runtime.NewVersionedType(v1alpha1.SignConfigType, v1alpha1.Version))

	sigInfo, err := h.Sign(t.Context(), digest, signCfg, map[string]string{
		credOIDCToken: stack.OIDCToken,
	})
	r.NoError(err, "signing should succeed")

	// --- Assert the bundle contains all material for offline verification ---

	bundle := decodeBundle(t, sigInfo)

	r.NotNil(bundle.VerificationMaterial.Certificate,
		"bundle must contain a Fulcio certificate for offline identity verification")
	r.NotEmpty(bundle.VerificationMaterial.Certificate.RawBytes)

	r.NotEmpty(bundle.VerificationMaterial.TlogEntries,
		"bundle must contain tlog entries for offline transparency verification")
	for i, raw := range bundle.VerificationMaterial.TlogEntries {
		var entry map[string]any
		r.NoError(json.Unmarshal(raw, &entry), "tlog entry %d must be valid JSON", i)
		r.Contains(entry, "inclusionProof", "tlog entry %d must have an inclusion proof for offline verification", i)
	}

	r.NotNil(bundle.MessageSignature, "bundle must contain the message signature")
	r.NotEmpty(bundle.MessageSignature.Signature)

	r.Equal(v1alpha1.AlgorithmSigstore, sigInfo.Algorithm)
	r.Equal(v1alpha1.MediaTypeSigstoreBundle, sigInfo.MediaType)
	r.NotEmpty(sigInfo.Issuer)

	// --- Verify offline: only bundle + trusted root, exact identity match ---

	signed := descruntime.Signature{
		Name:      "offline-verification-test",
		Digest:    digest,
		Signature: sigInfo,
	}

	verifyCfg := &v1alpha1.VerifyConfig{
		TrustedRoot:           stack.TrustedRootPath,
		CertificateOIDCIssuer: stack.OIDCIssuer,
		CertificateIdentity:   stack.OIDCIdentity,
	}
	verifyCfg.SetType(runtime.NewVersionedType(v1alpha1.VerifyConfigType, v1alpha1.Version))

	err = h.Verify(t.Context(), signed, verifyCfg, map[string]string{
		credTrustedRootJSONFile: stack.TrustedRootPath,
	})
	r.NoError(err, "offline verification using only bundle + trusted root should succeed")

	// --- Negative: wrong identity must fail even with valid bundle ---

	t.Run("wrong issuer fails offline", func(t *testing.T) {
		r := require.New(t)
		badCfg := &v1alpha1.VerifyConfig{
			TrustedRoot:           stack.TrustedRootPath,
			CertificateOIDCIssuer: "https://wrong-issuer.example.com",
			CertificateIdentity:   stack.OIDCIdentity,
		}
		badCfg.SetType(runtime.NewVersionedType(v1alpha1.VerifyConfigType, v1alpha1.Version))

		err := h.Verify(t.Context(), signed, badCfg, map[string]string{
			credTrustedRootJSONFile: stack.TrustedRootPath,
		})
		r.Error(err, "verification with wrong issuer must fail")
	})

	t.Run("wrong identity fails offline", func(t *testing.T) {
		r := require.New(t)
		badCfg := &v1alpha1.VerifyConfig{
			TrustedRoot:           stack.TrustedRootPath,
			CertificateOIDCIssuer: stack.OIDCIssuer,
			CertificateIdentity:   "wrong@example.com",
		}
		badCfg.SetType(runtime.NewVersionedType(v1alpha1.VerifyConfigType, v1alpha1.Version))

		err := h.Verify(t.Context(), signed, badCfg, map[string]string{
			credTrustedRootJSONFile: stack.TrustedRootPath,
		})
		r.Error(err, "verification with wrong identity must fail")
	})
}
