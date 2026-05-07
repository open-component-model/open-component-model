package integration_test

// TODO(ocm-project): Add integration tests for:
// - Public-good Sigstore infrastructure (requires real Fulcio/Rekor, not scaffolding)
// - GitHub Actions OIDC flow (requires GH runner with OIDC token endpoint)

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"

	"ocm.software/open-component-model/bindings/go/sigstore/signing/handler"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/v1alpha1"
)

//nolint:gosec // these are not secrets
const (
	credOIDCToken           = handler.CredentialKeyOIDCToken
	credTrustedRootJSONFile = handler.CredentialKeyTrustedRootJSONFile
)

// sigstoreEnv holds the environment configuration for the sigstore integration
// tests. All values are read from environment variables in TestMain, which are
// expected to be set by the scaffolding setup (either via CI or locally via
// hack/extract-sigstore-env.sh).
type sigstoreEnv struct {
	OIDCToken         string
	SigningConfigPath string
	TrustedRootPath   string
	OIDCIssuer        string
	OIDCIdentity      string
}

// stack is the shared sigstore environment used by all tests.
var stack *sigstoreEnv

func TestMain(m *testing.M) {
	stack = &sigstoreEnv{
		OIDCToken:         requireEnv("SIGSTORE_OIDC_TOKEN"),
		SigningConfigPath: requireEnv("SIGSTORE_SIGNING_CONFIG"),
		TrustedRootPath:   requireEnv("SIGSTORE_TRUSTED_ROOT"),
		OIDCIssuer:        requireEnv("SIGSTORE_OIDC_ISSUER"),
		OIDCIdentity:      requireEnv("SIGSTORE_OIDC_IDENTITY"),
	}
	os.Exit(m.Run())
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env var %s is not set", key)
	}
	return v
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

func newHandler(t *testing.T) *handler.Handler {
	t.Helper()
	return handler.New()
}

func defaultSignConfig() *v1alpha1.SignConfig {
	cfg := &v1alpha1.SignConfig{
		SigningConfig: stack.SigningConfigPath,
	}
	cfg.SetType(runtime.NewVersionedType(v1alpha1.SignConfigType, v1alpha1.Version))
	return cfg
}

func signDigest(t *testing.T, h *handler.Handler, digest descruntime.Digest) descruntime.SignatureInfo {
	t.Helper()
	sigInfo, err := h.Sign(t.Context(), digest, defaultSignConfig(), map[string]string{
		credOIDCToken: stack.OIDCToken,
	})
	require.NoError(t, err, "signing should succeed")
	return sigInfo
}

func testSignature(t *testing.T, h *handler.Handler, name, label string) descruntime.Signature {
	t.Helper()
	digest := uniqueDigest(t, label)
	sigInfo := signDigest(t, h, digest)
	return descruntime.Signature{
		Name:      name,
		Digest:    digest,
		Signature: sigInfo,
	}
}

func verifyConfig(opts ...func(*v1alpha1.VerifyConfig)) *v1alpha1.VerifyConfig {
	cfg := &v1alpha1.VerifyConfig{
		CertificateOIDCIssuer: stack.OIDCIssuer,
		CertificateIdentity:   stack.OIDCIdentity,
	}
	for _, o := range opts {
		o(cfg)
	}
	cfg.SetType(runtime.NewVersionedType(v1alpha1.VerifyConfigType, v1alpha1.Version))
	return cfg
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func Test_Integration_Keyless_IdentityVerification(t *testing.T) {
	h := newHandler(t)
	signed := testSignature(t, h, "identity-test", "identity-verify")
	r := require.New(t)
	r.NotEmpty(signed.Signature.Issuer)

	t.Run("matching issuer succeeds", func(t *testing.T) {
		r := require.New(t)
		cfg := verifyConfig(func(c *v1alpha1.VerifyConfig) {
			c.CertificateOIDCIssuer = signed.Signature.Issuer
		})
		r.NoError(h.Verify(t.Context(), signed, cfg, nil))
	})

	t.Run("wrong issuer fails", func(t *testing.T) {
		r := require.New(t)
		cfg := verifyConfig(func(c *v1alpha1.VerifyConfig) {
			c.CertificateOIDCIssuer = "https://wrong-issuer.example.com"
		})
		err := h.Verify(t.Context(), signed, cfg, nil)
		r.Error(err)
		r.ErrorContains(err, "issuer")
	})

	t.Run("issuer regex succeeds", func(t *testing.T) {
		r := require.New(t)
		cfg := verifyConfig(func(c *v1alpha1.VerifyConfig) {
			c.CertificateOIDCIssuer = ""
			c.CertificateOIDCIssuerRegexp = ".*"
		})
		r.NoError(h.Verify(t.Context(), signed, cfg, nil))
	})

	t.Run("matching identity succeeds", func(t *testing.T) {
		r := require.New(t)
		cfg := verifyConfig(func(c *v1alpha1.VerifyConfig) {
			c.CertificateOIDCIssuer = ""
			c.CertificateOIDCIssuerRegexp = ".*"
			c.CertificateIdentityRegexp = ""
			c.CertificateIdentity = stack.OIDCIdentity
		})
		r.NoError(h.Verify(t.Context(), signed, cfg, nil))
	})

	t.Run("wrong identity fails", func(t *testing.T) {
		r := require.New(t)
		cfg := verifyConfig(func(c *v1alpha1.VerifyConfig) {
			c.CertificateOIDCIssuer = ""
			c.CertificateOIDCIssuerRegexp = ".*"
			c.CertificateIdentityRegexp = ""
			c.CertificateIdentity = "wrong@example.com"
		})
		r.Error(h.Verify(t.Context(), signed, cfg, nil))
	})
}

func Test_Integration_TamperedBundle(t *testing.T) {
	h := newHandler(t)
	signed := testSignature(t, h, "tamper-baseline", "tamper")
	verifyCfg := verifyConfig(func(c *v1alpha1.VerifyConfig) {
		c.CertificateIdentityRegexp = ""
		c.CertificateIdentity = stack.OIDCIdentity
	})

	r := require.New(t)
	r.NoError(h.Verify(t.Context(), signed, verifyCfg, nil), "baseline verification must succeed")

	mutateBundle := func(t *testing.T, f func(m map[string]any)) string {
		t.Helper()
		r := require.New(t)
		raw, err := base64.StdEncoding.DecodeString(signed.Signature.Value)
		r.NoError(err)
		var m map[string]any
		r.NoError(json.Unmarshal(raw, &m))
		f(m)
		modified, err := json.Marshal(m)
		r.NoError(err)
		return base64.StdEncoding.EncodeToString(modified)
	}

	tamperedSignature := func(name, value string) descruntime.Signature {
		return descruntime.Signature{
			Name:   name,
			Digest: signed.Digest,
			Signature: descruntime.SignatureInfo{
				Algorithm: signed.Signature.Algorithm,
				MediaType: signed.Signature.MediaType,
				Value:     value,
				Issuer:    signed.Signature.Issuer,
			},
		}
	}

	t.Run("mutated signature bytes rejected", func(t *testing.T) {
		r := require.New(t)
		b := decodeBundle(t, signed.Signature)
		r.NotNil(b.MessageSignature)
		sigBytes, err := base64.StdEncoding.DecodeString(b.MessageSignature.Signature)
		r.NoError(err)
		r.NotEmpty(sigBytes)
		sigBytes[len(sigBytes)-1] ^= 0xFF

		tampered := mutateBundle(t, func(m map[string]any) {
			ms := m["messageSignature"].(map[string]any)
			ms["signature"] = base64.StdEncoding.EncodeToString(sigBytes)
		})
		r.Error(h.Verify(t.Context(), tamperedSignature("tamper-sig-bytes", tampered), verifyCfg, nil))
	})

	t.Run("stripped certificate rejected", func(t *testing.T) {
		r := require.New(t)
		tampered := mutateBundle(t, func(m map[string]any) {
			vm := m["verificationMaterial"].(map[string]any)
			delete(vm, "certificate")
		})
		r.Error(h.Verify(t.Context(), tamperedSignature("tamper-strip-cert", tampered), verifyCfg, nil))
	})

	t.Run("stripped tlog entries rejected", func(t *testing.T) {
		r := require.New(t)
		tampered := mutateBundle(t, func(m map[string]any) {
			vm := m["verificationMaterial"].(map[string]any)
			vm["tlogEntries"] = []any{}
		})
		r.Error(h.Verify(t.Context(), tamperedSignature("tamper-strip-tlog", tampered), verifyCfg, nil))
	})

	t.Run("wrong digest rejected", func(t *testing.T) {
		r := require.New(t)
		wrongDigest := uniqueDigest(t, "tamper-wrong-digest-other")
		s := descruntime.Signature{
			Name:      "tamper-wrong-digest",
			Digest:    wrongDigest,
			Signature: signed.Signature,
		}
		err := h.Verify(t.Context(), s, verifyCfg, nil)
		r.Error(err)
		r.ErrorContains(err, "verif")
	})

	t.Run("corrupted bundle rejected", func(t *testing.T) {
		r := require.New(t)
		garbage := base64.StdEncoding.EncodeToString([]byte(`{"not":"a valid bundle"}`))
		r.Error(h.Verify(t.Context(), tamperedSignature("tamper-corrupt-bundle", garbage), verifyCfg, nil))
	})
}

func Test_Integration_SignAndVerify(t *testing.T) {
	h := newHandler(t)
	digest := uniqueDigest(t, "sign-and-verify")
	sigInfo := signDigest(t, h, digest)
	r := require.New(t)

	bundle := decodeBundle(t, sigInfo)
	r.NotNil(bundle.VerificationMaterial.Certificate)
	r.NotEmpty(bundle.VerificationMaterial.Certificate.RawBytes)
	r.NotEmpty(bundle.VerificationMaterial.TlogEntries)
	for i, raw := range bundle.VerificationMaterial.TlogEntries {
		var entry map[string]any
		r.NoError(json.Unmarshal(raw, &entry), "tlog entry %d must be valid JSON", i)
		r.Contains(entry, "inclusionProof", "tlog entry %d must have an inclusion proof", i)
	}
	r.NotNil(bundle.MessageSignature)
	r.NotEmpty(bundle.MessageSignature.Signature)
	r.Equal(v1alpha1.AlgorithmSigstore, sigInfo.Algorithm)
	r.Equal(v1alpha1.MediaTypeSigstoreBundle, sigInfo.MediaType)
	r.NotEmpty(sigInfo.Issuer)

	signed := descruntime.Signature{
		Name:      "sign-and-verify-test",
		Digest:    digest,
		Signature: sigInfo,
	}

	verifyCfg := verifyConfig(func(c *v1alpha1.VerifyConfig) {
		c.CertificateIdentityRegexp = ""
		c.CertificateIdentity = stack.OIDCIdentity
	})
	r.NoError(h.Verify(t.Context(), signed, verifyCfg, nil))

	t.Run("wrong issuer fails", func(t *testing.T) {
		r := require.New(t)
		cfg := verifyConfig(func(c *v1alpha1.VerifyConfig) {
			c.CertificateOIDCIssuer = "https://wrong-issuer.example.com"
			c.CertificateIdentityRegexp = ""
			c.CertificateIdentity = stack.OIDCIdentity
		})
		err := h.Verify(t.Context(), signed, cfg, nil)
		r.Error(err)
		r.ErrorContains(err, "issuer")
	})

	t.Run("wrong identity fails", func(t *testing.T) {
		r := require.New(t)
		cfg := verifyConfig(func(c *v1alpha1.VerifyConfig) {
			c.CertificateIdentityRegexp = ""
			c.CertificateIdentity = "wrong@example.com"
		})
		r.Error(h.Verify(t.Context(), signed, cfg, nil))
	})
}

func Test_Integration_SignWithTrustedRoot(t *testing.T) {
	h := newHandler(t)
	digest := uniqueDigest(t, "sign-with-trusted-root")
	r := require.New(t)

	signCfg := &v1alpha1.SignConfig{
		SigningConfig: stack.SigningConfigPath,
	}
	signCfg.SetType(runtime.NewVersionedType(v1alpha1.SignConfigType, v1alpha1.Version))

	sigInfo, err := h.Sign(t.Context(), digest, signCfg, map[string]string{
		credOIDCToken:           stack.OIDCToken,
		credTrustedRootJSONFile: stack.TrustedRootPath,
	})
	r.NoError(err)

	bundle := decodeBundle(t, sigInfo)
	r.NotNil(bundle.VerificationMaterial.Certificate)
	r.NotEmpty(bundle.VerificationMaterial.TlogEntries)
	r.NotNil(bundle.MessageSignature)
	r.Equal(v1alpha1.AlgorithmSigstore, sigInfo.Algorithm)
	r.Equal(v1alpha1.MediaTypeSigstoreBundle, sigInfo.MediaType)
	r.NotEmpty(sigInfo.Issuer)

	signed := descruntime.Signature{
		Name:      "sign-with-trusted-root-test",
		Digest:    digest,
		Signature: sigInfo,
	}

	t.Run("verify with TUF-cached trust succeeds", func(t *testing.T) {
		r := require.New(t)
		r.NoError(h.Verify(t.Context(), signed, verifyConfig(), nil))
	})

	t.Run("verify with explicit trusted root succeeds", func(t *testing.T) {
		r := require.New(t)
		err := h.Verify(t.Context(), signed, verifyConfig(), map[string]string{
			credTrustedRootJSONFile: stack.TrustedRootPath,
		})
		r.NoError(err)
	})
}

func Test_Integration_VerifyWithExplicitTrustedRoot(t *testing.T) {
	h := newHandler(t)
	signed := testSignature(t, h, "verify-explicit-trusted-root-test", "verify-explicit-trusted-root")

	t.Run("trusted root via credential file path", func(t *testing.T) {
		r := require.New(t)
		r.NoError(h.Verify(t.Context(), signed, verifyConfig(), map[string]string{
			credTrustedRootJSONFile: stack.TrustedRootPath,
		}))
	})

	t.Run("trusted root via inline credential JSON", func(t *testing.T) {
		r := require.New(t)
		trustedRootJSON, err := os.ReadFile(stack.TrustedRootPath)
		r.NoError(err)

		err = h.Verify(t.Context(), signed, verifyConfig(), map[string]string{
			handler.CredentialKeyTrustedRootJSON: string(trustedRootJSON),
		})
		r.NoError(err)
	})

	t.Run("wrong issuer fails with explicit trusted root", func(t *testing.T) {
		r := require.New(t)
		cfg := verifyConfig(func(c *v1alpha1.VerifyConfig) {
			c.CertificateOIDCIssuer = "https://wrong-issuer.example.com"
		})
		err := h.Verify(t.Context(), signed, cfg, map[string]string{
			credTrustedRootJSONFile: stack.TrustedRootPath,
		})
		r.Error(err)
		r.ErrorContains(err, "issuer")
	})
}

func Test_Integration_PrivateInfrastructure(t *testing.T) {
	h := newHandler(t)
	signed := testSignature(t, h, "private-infrastructure-test", "private-infrastructure")

	cfg := verifyConfig(func(c *v1alpha1.VerifyConfig) {
		c.PrivateInfrastructure = true
	})

	r := require.New(t)
	err := h.Verify(t.Context(), signed, cfg, map[string]string{
		credTrustedRootJSONFile: stack.TrustedRootPath,
	})
	r.NoError(err)
}

func Test_Integration_AmbientSIGSTORE_ID_TOKEN(t *testing.T) {
	t.Setenv("SIGSTORE_ID_TOKEN", stack.OIDCToken)

	h := newHandler(t)
	digest := uniqueDigest(t, "ambient-sigstore-id-token")

	sigInfo, err := h.Sign(t.Context(), digest, defaultSignConfig(), map[string]string{})
	require.NoError(t, err, "signing with ambient SIGSTORE_ID_TOKEN should succeed without credential")
	require.Equal(t, v1alpha1.AlgorithmSigstore, sigInfo.Algorithm)
	require.NotEmpty(t, sigInfo.Value)
	require.NotEmpty(t, sigInfo.Issuer)

	signed := descruntime.Signature{
		Name:      "ambient-sigstore-id-token-test",
		Digest:    digest,
		Signature: sigInfo,
	}

	r := require.New(t)
	r.NoError(h.Verify(t.Context(), signed, verifyConfig(), nil))
}
