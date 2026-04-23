package handler

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"

	"ocm.software/open-component-model/bindings/go/sigstore/signing/v1alpha1"
)

func TestSign_BuildsCorrectOpts(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	mock := &mockExecutor{signBundleJSON: fakeBundleJSON(t)}
	h := NewWithExecutor(mock)

	cfg := testSignConfig()
	creds := map[string]string{CredentialKeyOIDCToken: "test-token"}

	_, err := h.Sign(t.Context(), testDigest(), cfg, creds)
	r.NoError(err)
	r.True(mock.signCalled)
	r.Equal("test-token", mock.signOpts.IdentityToken)
	r.Equal("https://fulcio.example.com", mock.signOpts.FulcioURL)
	r.Equal("https://rekor.example.com", mock.signOpts.RekorURL)
	r.Equal("https://tsa.example.com/api/v1/timestamp", mock.signOpts.TimestampServerURL)
	r.Empty(mock.signOpts.SigningConfig)
}

func TestSign_WithSigningConfig(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	mock := &mockExecutor{signBundleJSON: fakeBundleJSON(t)}
	h := NewWithExecutor(mock)

	cfg := testSignConfig()
	cfg.SigningConfig = "/etc/sigstore/signing_config.json"
	creds := map[string]string{CredentialKeyOIDCToken: "test-token"}

	_, err := h.Sign(t.Context(), testDigest(), cfg, creds)
	r.NoError(err)
	r.True(mock.signCalled)
	r.Equal("/etc/sigstore/signing_config.json", mock.signOpts.SigningConfig)
}

func TestSign_SigningConfigSuppressesUseSigningConfigFalse(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	mock := &mockExecutor{signBundleJSON: fakeBundleJSON(t)}
	h := NewWithExecutor(mock)

	// testSignConfig sets FulcioURL and RekorURL — without the fix these would
	// trigger --use-signing-config=false even when SigningConfig is also set.
	cfg := testSignConfig()
	cfg.SigningConfig = "/etc/sigstore/signing_config.json"
	creds := map[string]string{CredentialKeyOIDCToken: "test-token"}

	_, err := h.Sign(t.Context(), testDigest(), cfg, creds)
	r.NoError(err)
	r.Equal("/etc/sigstore/signing_config.json", mock.signOpts.SigningConfig)
	// Individual URLs are still passed through (cosign uses them if signing-config
	// doesn't specify them, but we don't also disable signing-config auto-discovery).
	r.Equal("https://fulcio.example.com", mock.signOpts.FulcioURL)
}

func TestSign_WithoutToken_Fails(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	mock := &mockExecutor{signBundleJSON: fakeBundleJSON(t)}
	h := NewWithExecutor(mock)

	cfg := testSignConfig()
	creds := map[string]string{}

	_, err := h.Sign(t.Context(), testDigest(), cfg, creds)
	r.Error(err)
	r.Contains(err.Error(), "OIDC identity token required")
	r.False(mock.signCalled, "executor should not be called without a token")
}

func TestSign_PassesDigestBytesToExecutor(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	mock := &mockExecutor{signBundleJSON: fakeBundleJSON(t)}
	h := NewWithExecutor(mock)

	cfg := testSignConfig()
	digest := testDigest()

	_, err := h.Sign(t.Context(), digest, cfg, map[string]string{CredentialKeyOIDCToken: "test-token"})
	r.NoError(err)

	expectedBytes, err := hex.DecodeString(digest.Value)
	r.NoError(err)
	r.Equal(expectedBytes, mock.signData)
}

func TestSign_BundleEncoding(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	bundleData := fakeBundleJSON(t)
	mock := &mockExecutor{signBundleJSON: bundleData}
	h := NewWithExecutor(mock)

	cfg := testSignConfig()
	result, err := h.Sign(t.Context(), testDigest(), cfg, map[string]string{CredentialKeyOIDCToken: "test-token"})
	r.NoError(err)

	r.Equal(v1alpha1.AlgorithmSigstore, result.Algorithm)
	r.Equal(v1alpha1.MediaTypeSigstoreBundle, result.MediaType)

	decoded, err := base64.StdEncoding.DecodeString(result.Value)
	r.NoError(err)
	r.JSONEq(string(bundleData), string(decoded))
}

func TestSign_IssuerExtraction(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	expectedIssuer := "https://accounts.google.com"
	bundleData := fakeBundleJSONWithCert(t, expectedIssuer)

	mock := &mockExecutor{signBundleJSON: bundleData}
	h := NewWithExecutor(mock)

	cfg := testSignConfig()
	result, err := h.Sign(t.Context(), testDigest(), cfg, map[string]string{CredentialKeyOIDCToken: "test-token"})
	r.NoError(err)
	r.Equal(expectedIssuer, result.Issuer)
}

func TestSign_IssuerV2Extraction(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	expectedIssuer := "https://token.actions.githubusercontent.com"

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	r.NoError(err)

	asn1Issuer, err := asn1.Marshal(expectedIssuer)
	r.NoError(err)

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		BasicConstraintsValid: true,
		ExtraExtensions: []pkix.Extension{
			{Id: sigstoreIssuerV2OID, Value: asn1Issuer},
		},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	r.NoError(err)

	certB64 := base64.StdEncoding.EncodeToString(certDER)
	bundle := map[string]interface{}{
		"mediaType": "application/vnd.dev.sigstore.bundle.v0.3+json",
		"verificationMaterial": map[string]interface{}{
			"certificate": map[string]string{"rawBytes": certB64},
		},
		"messageSignature": map[string]interface{}{},
	}
	bundleData, err := json.Marshal(bundle)
	r.NoError(err)

	mock := &mockExecutor{signBundleJSON: bundleData}
	h := NewWithExecutor(mock)

	cfg := testSignConfig()
	result, err := h.Sign(t.Context(), testDigest(), cfg, map[string]string{CredentialKeyOIDCToken: "test-token"})
	r.NoError(err)
	r.Equal(expectedIssuer, result.Issuer)
}

func TestSign_CosignError(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	mock := &mockExecutor{signErr: fmt.Errorf("cosign sign-blob failed: exit status 1\nstderr: error signing")}
	h := NewWithExecutor(mock)

	cfg := testSignConfig()
	_, err := h.Sign(t.Context(), testDigest(), cfg, map[string]string{CredentialKeyOIDCToken: "test-token"})
	r.Error(err)
	r.Contains(err.Error(), "cosign sign")
}

func TestSign_InvalidHexDigest(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	mock := &mockExecutor{}
	h := NewWithExecutor(mock)

	cfg := testSignConfig()
	digest := descruntime.Digest{Value: "not-hex!"}
	_, err := h.Sign(t.Context(), digest, cfg, map[string]string{CredentialKeyOIDCToken: "test-token"})
	r.Error(err)
	r.Contains(err.Error(), "decode digest hex")
}

func TestSign_UnregisteredConfigType(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	h := NewWithExecutor(&mockExecutor{})

	cfg := &runtime.Raw{}
	cfg.SetType(runtime.NewVersionedType("UnknownConfig", "v1"))
	_, err := h.Sign(t.Context(), testDigest(), cfg, map[string]string{})
	r.Error(err)
	r.Contains(err.Error(), "convert config")
}

func TestSign_NonHTTPSURLRejected(t *testing.T) {
	t.Parallel()

	creds := map[string]string{CredentialKeyOIDCToken: "tok"}

	cases := []struct {
		name string
		cfg  func() *v1alpha1.SignConfig
	}{
		{
			name: "http FulcioURL",
			cfg: func() *v1alpha1.SignConfig {
				c := testSignConfig()
				c.FulcioURL = "http://fulcio.example.com"
				return c
			},
		},
		{
			name: "http RekorURL",
			cfg: func() *v1alpha1.SignConfig {
				c := testSignConfig()
				c.RekorURL = "http://rekor.example.com"
				return c
			},
		},
		{
			name: "http TimestampServerURL",
			cfg: func() *v1alpha1.SignConfig {
				c := testSignConfig()
				c.TimestampServerURL = "http://tsa.example.com"
				return c
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			mock := &mockExecutor{}
			h := NewWithExecutor(mock)
			_, err := h.Sign(t.Context(), testDigest(), tc.cfg(), creds)
			r.Error(err)
			r.False(mock.signCalled, "executor must not be invoked for invalid URL")
		})
	}
}
