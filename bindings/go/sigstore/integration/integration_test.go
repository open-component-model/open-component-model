package integration_test

// TODO(ocm-project): Add integration tests for:
// - Public-good Sigstore infrastructure (requires real Fulcio/Rekor, not scaffolding)
// - GitHub Actions OIDC flow (requires GH runner with OIDC token endpoint)
//
// This suite covers only the live SIGN path, which needs the scaffolding cluster
// (Fulcio/Rekor/TSA + the OIDC token service). The verification tests, which do
// not need live infrastructure, run offline against recorded fixtures in the
// sibling ./replay package.

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/handler"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/v1alpha1"
	oidcv1 "ocm.software/open-component-model/bindings/go/sigstore/spec/credentials/oidcidentitytoken/v1alpha1"
)

// sigstoreEnv holds the environment configuration for the sigstore integration
// tests. All values are read from environment variables in TestMain, which are
// expected to be set by the scaffolding setup (either via CI or locally via
// hack/extract-sigstore-env.sh).
type sigstoreEnv struct {
	OIDCToken         string
	SigningConfigPath string
	OIDCIssuer        string
	OIDCIdentity      string
}

// stack is the shared sigstore environment used by all tests.
var stack *sigstoreEnv

func TestMain(m *testing.M) {
	stack = &sigstoreEnv{
		OIDCToken:         requireEnv("SIGSTORE_OIDC_TOKEN"),
		SigningConfigPath: requireEnv("SIGSTORE_SIGNING_CONFIG"),
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
		TlogEntries               []jsontext.Value `json:"tlogEntries"`
		TimestampVerificationData jsontext.Value   `json:"timestampVerificationData,omitzero"`
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
	sigInfo, err := h.Sign(t.Context(), digest, defaultSignConfig(), &oidcv1.OIDCIdentityToken{
		Token: stack.OIDCToken,
	})
	require.NoError(t, err, "signing should succeed")
	return sigInfo
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
// Tests (live sign path)
// ---------------------------------------------------------------------------

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
	r.Equal(string(v1alpha1.AlgorithmSigstoreV1Alpha1), sigInfo.Algorithm)
	r.Equal(v1alpha1.MediaTypeSigstoreBundle, sigInfo.MediaType)

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

func Test_Integration_AmbientSIGSTORE_ID_TOKEN(t *testing.T) {
	t.Setenv("SIGSTORE_ID_TOKEN", stack.OIDCToken)

	h := newHandler(t)
	digest := uniqueDigest(t, "ambient-sigstore-id-token")

	sigInfo, err := h.Sign(t.Context(), digest, defaultSignConfig(), nil)
	require.NoError(t, err, "signing with ambient SIGSTORE_ID_TOKEN should succeed without credential")
	require.Equal(t, string(v1alpha1.AlgorithmSigstoreV1Alpha1), sigInfo.Algorithm)
	require.NotEmpty(t, sigInfo.Value)

	signed := descruntime.Signature{
		Name:      "ambient-sigstore-id-token-test",
		Digest:    digest,
		Signature: sigInfo,
	}

	r := require.New(t)
	r.NoError(h.Verify(t.Context(), signed, verifyConfig(), nil))
}
