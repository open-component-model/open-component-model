package handler

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/handler/internal"

	sigcredentials "ocm.software/open-component-model/bindings/go/sigstore/signing/handler/internal/credentials"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/v1alpha1"
)

// execRecorder captures args and env from ExecCosign calls and optionally writes a bundle file.
type execRecorder struct {
	lastSignArgs   []string
	lastSignEnv    []string
	lastVerifyArgs []string
	lastVerifyEnv  []string
	signErr        error
	verifyErr      error
	bundleJSON     []byte
}

func (m *execRecorder) exec(_ context.Context, _ string, args, env []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no args")
	}
	switch args[0] {
	case "sign-blob":
		m.lastSignArgs = args[1:]
		m.lastSignEnv = env
		if m.bundleJSON != nil {
			bundlePath := flagValue(args, "--bundle")
			if bundlePath != "" {
				if err := os.WriteFile(bundlePath, m.bundleJSON, 0o644); err != nil {
					return fmt.Errorf("mock: write bundle: %w", err)
				}
			}
		}
		return m.signErr
	case "verify-blob":
		m.lastVerifyArgs = args[1:]
		m.lastVerifyEnv = env
		return m.verifyErr
	default:
		return fmt.Errorf("unexpected subcommand: %s", args[0])
	}
}

func newSignMock(t *testing.T, bundleJSON []byte) *execRecorder {
	t.Helper()
	return &execRecorder{bundleJSON: bundleJSON}
}

func newWithRunner(runner *execRecorder, opts ...HandlerOption) *Handler {
	allOpts := []HandlerOption{
		WithLookPath(func(string) (string, error) { return "/fake/cosign", nil }),
		WithExecCosign(runner.exec),
	}
	allOpts = append(allOpts, opts...)
	return New(allOpts...)
}

// --- Test helpers ---

func flagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func testDigest() descruntime.Digest {
	return descruntime.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "jsonNormalisation/v2",
		Value:                  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}
}

func testSignConfig() *v1alpha1.SignConfig {
	cfg := &v1alpha1.SignConfig{
		SigningConfig: "/etc/sigstore/signing_config.json",
	}
	cfg.SetType(runtime.NewVersionedType(v1alpha1.SignConfigType, v1alpha1.Version))
	return cfg
}

func testVerifyConfig() *v1alpha1.VerifyConfig {
	cfg := &v1alpha1.VerifyConfig{
		CertificateOIDCIssuer: "https://accounts.google.com",
		CertificateIdentity:   "user@example.com",
	}
	cfg.SetType(runtime.NewVersionedType(v1alpha1.VerifyConfigType, v1alpha1.Version))
	return cfg
}

func fakeBundleJSON(t *testing.T) []byte {
	t.Helper()
	return fakeBundleJSONWithCert(t, "https://accounts.google.com")
}

func fakeBundleJSONWithCert(t *testing.T, issuer string) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		BasicConstraintsValid: true,
		EmailAddresses:        []string{"signer@example.com"},
	}

	issuerExtValue := []byte(issuer)
	template.ExtraExtensions = []pkix.Extension{
		{
			Id:    sigstoreIssuerV1OID,
			Value: issuerExtValue,
		},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	certB64 := base64.StdEncoding.EncodeToString(certDER)
	bundle := map[string]any{
		"mediaType": "application/vnd.dev.sigstore.bundle.v0.3+json",
		"verificationMaterial": map[string]any{
			"certificate": map[string]string{"rawBytes": certB64},
			"tlogEntries": []any{},
		},
		"messageSignature": map[string]any{
			"messageDigest": map[string]string{"algorithm": "SHA2_256", "digest": ""},
			"signature":     "",
		},
	}
	data, err := json.Marshal(bundle)
	require.NoError(t, err)
	return data
}

func fakeBundleJSONWithCertV2(t *testing.T, issuer string) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	asn1Issuer, err := asn1.Marshal(issuer)
	require.NoError(t, err)

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
	require.NoError(t, err)

	certB64 := base64.StdEncoding.EncodeToString(certDER)
	bundle := map[string]any{
		"mediaType": "application/vnd.dev.sigstore.bundle.v0.3+json",
		"verificationMaterial": map[string]any{
			"certificate": map[string]string{"rawBytes": certB64},
			"tlogEntries": []any{},
		},
		"messageSignature": map[string]any{
			"messageDigest": map[string]string{"algorithm": "SHA2_256", "digest": ""},
			"signature":     "",
		},
	}
	data, err := json.Marshal(bundle)
	require.NoError(t, err)
	return data
}

// hasArg checks whether the given flag appears in the args slice.
func hasArg(args []string, flag string) bool {
	return slices.Contains(args, flag)
}

// argValue returns the value of a --flag value pair from the args slice.
func argValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// hasEnvKey checks whether an env slice contains a key=... entry.
// envValue returns the value for a given key in an env slice.
func envValue(env []string, key string) string {
	prefix := key + "="
	for _, e := range env {
		if len(e) >= len(prefix) && e[:len(prefix)] == prefix {
			return e[len(prefix):]
		}
	}
	return ""
}

// --- Sign Tests ---

func TestHandler_Sign(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		cfg          func() *v1alpha1.SignConfig
		creds        map[string]string
		digest       func() descruntime.Digest
		bundleJSON   func(t *testing.T) []byte
		mockErr      error
		wantErr      string
		assertArgs   func(t *testing.T, args []string)
		assertEnv    func(t *testing.T, env []string)
		assertResult func(t *testing.T, result descruntime.SignatureInfo)
		assertMock   func(t *testing.T, mock *execRecorder)
	}{
		{
			name:  "builds correct args with signing config",
			creds: map[string]string{CredentialKeyOIDCToken: "test-token"},
			assertArgs: func(t *testing.T, args []string) {
				r := require.New(t)
				r.Equal("/etc/sigstore/signing_config.json", argValue(args, "--signing-config"))
			},
			assertEnv: func(t *testing.T, env []string) {
				r := require.New(t)
				r.True(internal.HasEnvKey(env, "SIGSTORE_ID_TOKEN"))
				r.Equal("test-token", envValue(env, "SIGSTORE_ID_TOKEN"))
			},
		},
		{
			name:    "missing OIDC token fails before executor call",
			creds:   map[string]string{},
			wantErr: "OIDC identity token required",
			assertMock: func(t *testing.T, mock *execRecorder) {
				require.Nil(t, mock.lastSignArgs)
			},
		},
		{
			name:  "invalid hex digest",
			creds: map[string]string{CredentialKeyOIDCToken: "test-token"},
			digest: func() descruntime.Digest {
				return descruntime.Digest{Value: "not-hex!"}
			},
			wantErr: "decode digest hex",
		},
		{
			name:  "empty digest value rejected",
			creds: map[string]string{CredentialKeyOIDCToken: "test-token"},
			digest: func() descruntime.Digest {
				return descruntime.Digest{Value: ""}
			},
			wantErr: "digest value must not be empty",
		},
		{
			name:  "OIDC token trimmed of whitespace",
			creds: map[string]string{CredentialKeyOIDCToken: "  test-token\n"},
			assertEnv: func(t *testing.T, env []string) {
				require.Equal(t, "test-token", envValue(env, "SIGSTORE_ID_TOKEN"))
			},
		},
		{
			name:    "executor error propagated",
			creds:   map[string]string{CredentialKeyOIDCToken: "test-token"},
			mockErr: fmt.Errorf("cosign sign-blob failed: exit status 1\nstderr: error signing"),
			wantErr: "cosign sign",
		},
		{
			name:  "bundle base64-encoded in result",
			creds: map[string]string{CredentialKeyOIDCToken: "test-token"},
			assertResult: func(t *testing.T, result descruntime.SignatureInfo) {
				r := require.New(t)
				r.Equal(v1alpha1.AlgorithmSigstore, result.Algorithm)
				r.Equal(v1alpha1.MediaTypeSigstoreBundle, result.MediaType)
				decoded, err := base64.StdEncoding.DecodeString(result.Value)
				r.NoError(err)
				r.NotEmpty(decoded)
			},
		},
		{
			name:       "V1 issuer extracted from bundle",
			creds:      map[string]string{CredentialKeyOIDCToken: "test-token"},
			bundleJSON: func(t *testing.T) []byte { return fakeBundleJSONWithCert(t, "https://accounts.google.com") },
			assertResult: func(t *testing.T, result descruntime.SignatureInfo) {
				require.Equal(t, "https://accounts.google.com", result.Issuer)
			},
		},
		{
			name:  "V2 issuer extracted from bundle",
			creds: map[string]string{CredentialKeyOIDCToken: "test-token"},
			bundleJSON: func(t *testing.T) []byte {
				return fakeBundleJSONWithCertV2(t, "https://token.actions.githubusercontent.com")
			},
			assertResult: func(t *testing.T, result descruntime.SignatureInfo) {
				require.Equal(t, "https://token.actions.githubusercontent.com", result.Issuer)
			},
		},
		{
			name:  "trusted root from credential file passed as flag on sign",
			creds: map[string]string{CredentialKeyOIDCToken: "test-token", CredentialKeyTrustedRootJSONFile: "/path/to/trusted_root.json"},
			assertArgs: func(t *testing.T, args []string) {
				require.Equal(t, "/path/to/trusted_root.json", argValue(args, "--trusted-root"))
			},
		},
		{
			name:  "trusted root from inline JSON credential passed as flag on sign",
			creds: map[string]string{CredentialKeyOIDCToken: "test-token", CredentialKeyTrustedRootJSON: `{"mediaType":"application/vnd.dev.sigstore.trustedroot+json;version=0.1"}`},
			assertArgs: func(t *testing.T, args []string) {
				r := require.New(t)
				r.True(hasArg(args, "--trusted-root"))
				r.NotEmpty(argValue(args, "--trusted-root"))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			bundleJSON := fakeBundleJSON(t)
			if tc.bundleJSON != nil {
				bundleJSON = tc.bundleJSON(t)
			}

			mock := newSignMock(t, bundleJSON)
			mock.signErr = tc.mockErr
			h := newWithRunner(mock)

			cfg := testSignConfig()
			if tc.cfg != nil {
				cfg = tc.cfg()
			}

			digest := testDigest()
			if tc.digest != nil {
				digest = tc.digest()
			}

			creds := tc.creds
			if creds == nil {
				creds = map[string]string{CredentialKeyOIDCToken: "test-token"}
			}

			result, err := h.Sign(t.Context(), digest, cfg, creds)

			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				if tc.assertMock != nil {
					tc.assertMock(t, mock)
				}
				return
			}
			r.NoError(err)

			if tc.assertArgs != nil {
				tc.assertArgs(t, mock.lastSignArgs)
			}
			if tc.assertEnv != nil {
				tc.assertEnv(t, mock.lastSignEnv)
			}
			if tc.assertResult != nil {
				tc.assertResult(t, result)
			}
			if tc.assertMock != nil {
				tc.assertMock(t, mock)
			}
		})
	}
}

func TestSign_UnregisteredConfigType(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	h := newWithRunner(&execRecorder{})

	cfg := &runtime.Raw{}
	cfg.SetType(runtime.NewVersionedType("UnknownConfig", "v1"))
	_, err := h.Sign(t.Context(), testDigest(), cfg, map[string]string{})
	r.Error(err)
	r.Contains(err.Error(), "convert config")
}

func TestSign_AmbientSIGSTORE_ID_TOKEN(t *testing.T) {
	t.Setenv("SIGSTORE_ID_TOKEN", "ambient-token-from-env")

	mock := newSignMock(t, fakeBundleJSON(t))
	h := newWithRunner(mock)

	result, err := h.Sign(t.Context(), testDigest(), testSignConfig(), map[string]string{})
	require.NoError(t, err)
	require.NotNil(t, mock.lastSignArgs)
	require.Equal(t, "ambient-token-from-env", envValue(mock.lastSignEnv, "SIGSTORE_ID_TOKEN"))
	require.NotEmpty(t, result.Value)
}

func TestSign_AmbientACTIONS_ID_TOKEN_vars(t *testing.T) {
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "ghs_fakeRunnerJWT")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "https://token.actions.githubusercontent.com")

	mock := newSignMock(t, fakeBundleJSON(t))
	h := newWithRunner(mock)

	_, err := h.Sign(t.Context(), testDigest(), testSignConfig(), map[string]string{})
	require.NoError(t, err)
	require.Equal(t, "ghs_fakeRunnerJWT", envValue(mock.lastSignEnv, "ACTIONS_ID_TOKEN_REQUEST_TOKEN"))
	require.Equal(t, "https://token.actions.githubusercontent.com", envValue(mock.lastSignEnv, "ACTIONS_ID_TOKEN_REQUEST_URL"))
	require.False(t, internal.HasEnvKey(mock.lastSignEnv, "SIGSTORE_ID_TOKEN"), "should not inject SIGSTORE_ID_TOKEN when Actions OIDC is available")
}

func TestSign_TUF_ROOT_DoesNotSuppressTrustedRootFlag(t *testing.T) {
	t.Setenv("TUF_ROOT", "/tuf/cache")

	mock := newSignMock(t, fakeBundleJSON(t))
	h := newWithRunner(mock)

	creds := map[string]string{
		CredentialKeyOIDCToken:           "test-token",
		CredentialKeyTrustedRootJSONFile: "/cred/root.json",
	}
	_, err := h.Sign(t.Context(), testDigest(), testSignConfig(), creds)
	require.NoError(t, err)
	require.Equal(t, "/cred/root.json", argValue(mock.lastSignArgs, "--trusted-root"),
		"TUF_ROOT is for TUF cache, not trusted root — credential should still produce --trusted-root")
}

func TestVerify_TUF_ROOT_DoesNotSuppressTrustedRootFlag(t *testing.T) {
	t.Setenv("TUF_ROOT", "/tuf/cache")

	mock := &execRecorder{}
	h := newWithRunner(mock)

	cfg := testVerifyConfig()
	creds := map[string]string{
		CredentialKeyTrustedRootJSONFile: "/cred/root.json",
	}

	bundleJSON := fakeBundleJSON(t)
	signed := descruntime.Signature{
		Name:   "test-sig",
		Digest: testDigest(),
		Signature: descruntime.SignatureInfo{
			Algorithm: v1alpha1.AlgorithmSigstore,
			MediaType: v1alpha1.MediaTypeSigstoreBundle,
			Value:     base64.StdEncoding.EncodeToString(bundleJSON),
		},
	}

	err := h.Verify(t.Context(), signed, cfg, creds)
	require.NoError(t, err)
	require.Equal(t, "/cred/root.json", argValue(mock.lastVerifyArgs, "--trusted-root"),
		"TUF_ROOT is for TUF cache, not trusted root — credential should still produce --trusted-root")
}

// --- Verify Tests ---

func TestHandler_Verify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		cfgSetup   func(cfg *v1alpha1.VerifyConfig)
		creds      map[string]string
		mockErr    error
		wantErr    string
		assertArgs func(t *testing.T, args []string)
		assertMock func(t *testing.T, mock *execRecorder)
	}{
		{
			name: "exact issuer and identity",
			assertArgs: func(t *testing.T, args []string) {
				r := require.New(t)
				r.Equal("user@example.com", argValue(args, "--certificate-identity"))
				r.Equal("https://accounts.google.com", argValue(args, "--certificate-oidc-issuer"))
				r.False(hasArg(args, "--certificate-identity-regexp"))
				r.False(hasArg(args, "--certificate-oidc-issuer-regexp"))
			},
		},
		{
			name: "regexp issuer and identity",
			cfgSetup: func(cfg *v1alpha1.VerifyConfig) {
				cfg.CertificateOIDCIssuer = ""
				cfg.CertificateIdentity = ""
				cfg.CertificateOIDCIssuerRegexp = ".*google.*"
				cfg.CertificateIdentityRegexp = ".*@example.com"
			},
			creds: map[string]string{
				CredentialKeyTrustedRootJSONFile: "/path/to/trusted_root.json",
			},
			assertArgs: func(t *testing.T, args []string) {
				r := require.New(t)
				r.False(hasArg(args, "--certificate-identity"))
				r.False(hasArg(args, "--certificate-oidc-issuer"))
				r.Equal(".*@example.com", argValue(args, "--certificate-identity-regexp"))
				r.Equal(".*google.*", argValue(args, "--certificate-oidc-issuer-regexp"))
				r.Equal("/path/to/trusted_root.json", argValue(args, "--trusted-root"))
			},
		},
		{
			name: "private infrastructure",
			cfgSetup: func(cfg *v1alpha1.VerifyConfig) {
				cfg.PrivateInfrastructure = true
			},
			creds: map[string]string{
				CredentialKeyTrustedRootJSONFile: "/path/to/private_trusted_root.json",
			},
			assertArgs: func(t *testing.T, args []string) {
				r := require.New(t)
				r.True(hasArg(args, "--private-infrastructure"))
				r.Equal("/path/to/private_trusted_root.json", argValue(args, "--trusted-root"))
			},
		},
		{
			name: "trusted root from inline JSON credential",
			creds: map[string]string{
				CredentialKeyTrustedRootJSON: `{"mediaType":"application/vnd.dev.sigstore.trustedroot+json;version=0.1"}`,
			},
			assertArgs: func(t *testing.T, args []string) {
				r := require.New(t)
				r.True(hasArg(args, "--trusted-root"))
				r.NotEmpty(argValue(args, "--trusted-root"))
			},
		},
		{
			name: "trusted root from file credential",
			creds: map[string]string{
				CredentialKeyTrustedRootJSONFile: "/custom/path/trusted_root.json",
			},
			assertArgs: func(t *testing.T, args []string) {
				require.Equal(t, "/custom/path/trusted_root.json", argValue(args, "--trusted-root"))
			},
		},
		{
			name: "no trusted root yields no flag",
			assertArgs: func(t *testing.T, args []string) {
				require.False(t, hasArg(args, "--trusted-root"))
			},
		},
		{
			name:    "executor error propagated",
			mockErr: fmt.Errorf("cosign verify-blob failed: exit status 1\nstderr: verification failed"),
			wantErr: "cosign verify-blob failed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			mock := &execRecorder{verifyErr: tc.mockErr}
			h := newWithRunner(mock)

			cfg := testVerifyConfig()
			if tc.cfgSetup != nil {
				tc.cfgSetup(cfg)
			}

			creds := tc.creds
			if creds == nil {
				creds = map[string]string{}
			}

			bundleJSON := fakeBundleJSON(t)
			signed := descruntime.Signature{
				Name:   "test-sig",
				Digest: testDigest(),
				Signature: descruntime.SignatureInfo{
					Algorithm: v1alpha1.AlgorithmSigstore,
					MediaType: v1alpha1.MediaTypeSigstoreBundle,
					Value:     base64.StdEncoding.EncodeToString(bundleJSON),
				},
			}

			err := h.Verify(t.Context(), signed, cfg, creds)

			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.NotNil(mock.lastVerifyArgs)

			if tc.assertArgs != nil {
				tc.assertArgs(t, mock.lastVerifyArgs)
			}
			if tc.assertMock != nil {
				tc.assertMock(t, mock)
			}
		})
	}
}

func TestVerify_MissingIdentity(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	h := newWithRunner(&execRecorder{})

	cfg := &v1alpha1.VerifyConfig{}
	cfg.SetType(runtime.NewVersionedType(v1alpha1.VerifyConfigType, v1alpha1.Version))

	bundleJSON := fakeBundleJSON(t)
	signed := descruntime.Signature{
		Name:   "test-sig",
		Digest: testDigest(),
		Signature: descruntime.SignatureInfo{
			Algorithm: v1alpha1.AlgorithmSigstore,
			MediaType: v1alpha1.MediaTypeSigstoreBundle,
			Value:     base64.StdEncoding.EncodeToString(bundleJSON),
		},
	}

	err := h.Verify(t.Context(), signed, cfg, map[string]string{})
	r.Error(err)
	r.Contains(err.Error(), "keyless verification requires")
}

func TestVerify_PrivateInfrastructureWithoutTrustedRoot(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	h := newWithRunner(&execRecorder{})

	cfg := testVerifyConfig()
	cfg.PrivateInfrastructure = true

	bundleJSON := fakeBundleJSON(t)
	signed := descruntime.Signature{
		Name:   "test-sig",
		Digest: testDigest(),
		Signature: descruntime.SignatureInfo{
			Algorithm: v1alpha1.AlgorithmSigstore,
			MediaType: v1alpha1.MediaTypeSigstoreBundle,
			Value:     base64.StdEncoding.EncodeToString(bundleJSON),
		},
	}

	err := h.Verify(t.Context(), signed, cfg, map[string]string{})
	r.Error(err)
	r.Contains(err.Error(), "privateInfrastructure requires a trusted root")
}

func TestVerify_EmptyDigestRejected(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	h := newWithRunner(&execRecorder{})

	cfg := testVerifyConfig()

	bundleJSON := fakeBundleJSON(t)
	signed := descruntime.Signature{
		Name:   "test-sig",
		Digest: descruntime.Digest{Value: ""},
		Signature: descruntime.SignatureInfo{
			Algorithm: v1alpha1.AlgorithmSigstore,
			MediaType: v1alpha1.MediaTypeSigstoreBundle,
			Value:     base64.StdEncoding.EncodeToString(bundleJSON),
		},
	}

	err := h.Verify(t.Context(), signed, cfg, map[string]string{})
	r.Error(err)
	r.Contains(err.Error(), "digest value must not be empty")
}

func TestVerify_PrivateInfrastructureWithTrustedRootCredential(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	mock := &execRecorder{}
	h := newWithRunner(mock)

	cfg := testVerifyConfig()
	cfg.PrivateInfrastructure = true

	creds := map[string]string{
		CredentialKeyTrustedRootJSON: `{"mediaType":"application/vnd.dev.sigstore.trustedroot+json;version=0.1"}`,
	}

	bundleJSON := fakeBundleJSON(t)
	signed := descruntime.Signature{
		Name:   "test-sig",
		Digest: testDigest(),
		Signature: descruntime.SignatureInfo{
			Algorithm: v1alpha1.AlgorithmSigstore,
			MediaType: v1alpha1.MediaTypeSigstoreBundle,
			Value:     base64.StdEncoding.EncodeToString(bundleJSON),
		},
	}

	err := h.Verify(t.Context(), signed, cfg, creds)
	r.NoError(err)
	r.NotNil(mock.lastVerifyArgs)
	r.True(hasArg(mock.lastVerifyArgs, "--private-infrastructure"))
}

func TestVerify_CertificateOIDCIssuerAcceptsHTTP(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	mock := &execRecorder{}
	h := newWithRunner(mock)

	cfg := testVerifyConfig()
	cfg.CertificateOIDCIssuer = "http://accounts.google.com"

	bundleJSON := fakeBundleJSON(t)
	signed := descruntime.Signature{
		Name:   "test-sig",
		Digest: testDigest(),
		Signature: descruntime.SignatureInfo{
			Algorithm: v1alpha1.AlgorithmSigstore,
			MediaType: v1alpha1.MediaTypeSigstoreBundle,
			Value:     base64.StdEncoding.EncodeToString(bundleJSON),
		},
	}

	err := h.Verify(t.Context(), signed, cfg, map[string]string{})
	r.NoError(err)
	r.NotNil(mock.lastVerifyArgs)
}

func TestVerify_InvalidBase64Bundle(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	h := newWithRunner(&execRecorder{})
	cfg := testVerifyConfig()
	signed := descruntime.Signature{
		Name:   "test-sig",
		Digest: testDigest(),
		Signature: descruntime.SignatureInfo{
			Algorithm: v1alpha1.AlgorithmSigstore,
			MediaType: v1alpha1.MediaTypeSigstoreBundle,
			Value:     "not-valid-base64!!!",
		},
	}

	err := h.Verify(t.Context(), signed, cfg, map[string]string{})
	r.Error(err)
	r.Contains(err.Error(), "decode bundle base64")
}

func TestVerify_UnregisteredConfigType(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	h := newWithRunner(&execRecorder{})
	cfg := &runtime.Raw{}
	cfg.SetType(runtime.NewVersionedType("UnknownConfig", "v1"))
	signed := descruntime.Signature{
		Name:   "test-sig",
		Digest: testDigest(),
		Signature: descruntime.SignatureInfo{
			Algorithm: v1alpha1.AlgorithmSigstore,
			MediaType: v1alpha1.MediaTypeSigstoreBundle,
			Value:     base64.StdEncoding.EncodeToString(fakeBundleJSON(t)),
		},
	}

	err := h.Verify(t.Context(), signed, cfg, map[string]string{})
	r.Error(err)
	r.Contains(err.Error(), "convert config")
}

func TestVerify_UnsupportedMediaType(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	h := newWithRunner(&execRecorder{})
	cfg := testVerifyConfig()
	signed := descruntime.Signature{
		Name:   "test-sig",
		Digest: testDigest(),
		Signature: descruntime.SignatureInfo{
			Algorithm: "RSA-PSS",
			MediaType: "application/pgp-signature",
			Value:     "irrelevant",
		},
	}

	err := h.Verify(t.Context(), signed, cfg, map[string]string{})
	r.Error(err)
	r.Contains(err.Error(), "unsupported media type")
}

// --- ResolveTrustedRootPath Tests ---

func TestResolveTrustedRootPath(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		creds   map[string]string
		want    string
		wantErr string
		isFile  bool // if true, assert path exists as a file (written from inline JSON)
	}{
		{
			name:   "inline JSON wins over file credential",
			creds:  map[string]string{CredentialKeyTrustedRootJSON: `{"mediaType":"test"}`, CredentialKeyTrustedRootJSONFile: "/cred/root.json"},
			isFile: true,
		},
		{
			name:  "file credential used",
			creds: map[string]string{CredentialKeyTrustedRootJSONFile: "/cred/root.json"},
			want:  "/cred/root.json",
		},
		{
			name:  "empty when nothing set",
			creds: map[string]string{},
			want:  "",
		},
		{
			name:    "relative path rejected",
			creds:   map[string]string{CredentialKeyTrustedRootJSONFile: "../../etc/passwd"},
			wantErr: "must be absolute",
		},
		{
			name:    "path traversal rejected",
			creds:   map[string]string{CredentialKeyTrustedRootJSONFile: "/legit/../../../etc/passwd"},
			wantErr: "non-canonical",
		},
		{
			name:  "whitespace-only JSON treated as empty",
			creds: map[string]string{CredentialKeyTrustedRootJSON: "   \n\t  "},
			want:  "",
		},
		{
			name:  "whitespace-only file path treated as empty",
			creds: map[string]string{CredentialKeyTrustedRootJSONFile: "   "},
			want:  "",
		},
		{
			name:  "valid absolute path accepted",
			creds: map[string]string{CredentialKeyTrustedRootJSONFile: "/opt/sigstore/trusted_root.json"},
			want:  "/opt/sigstore/trusted_root.json",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			path, err := resolveTrustedRootPath(tc.creds, tmpDir)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			if tc.isFile {
				r.FileExists(path)
			} else {
				r.Equal(tc.want, path)
			}
		})
	}
}

// --- Credential Identity Tests ---

func TestGetSigningCredentialConsumerIdentity(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	h := newWithRunner(&execRecorder{})
	cfg := testSignConfig()

	id, err := h.GetSigningCredentialConsumerIdentity(t.Context(), "my-sig", testDigest(), cfg)
	r.NoError(err)
	r.Equal(v1alpha1.AlgorithmSigstore, id[IdentityAttributeAlgorithm])
	r.Equal("my-sig", id[IdentityAttributeSignature])
	r.Equal(sigcredentials.IdentityTypeSigstoreSigner, id.GetType())
	r.Len(id, 3)
}

func TestGetVerifyingCredentialConsumerIdentity(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	h := newWithRunner(&execRecorder{})
	signed := descruntime.Signature{
		Name: "my-sig",
		Signature: descruntime.SignatureInfo{
			MediaType: v1alpha1.MediaTypeSigstoreBundle,
		},
	}

	id, err := h.GetVerifyingCredentialConsumerIdentity(t.Context(), signed, nil)
	r.NoError(err)
	r.Equal(v1alpha1.AlgorithmSigstore, id[IdentityAttributeAlgorithm])
	r.Equal("my-sig", id[IdentityAttributeSignature])
	r.Equal(sigcredentials.IdentityTypeSigstoreVerifier, id.GetType())
	r.Len(id, 3)
}

func TestGetVerifyingCredentialConsumerIdentity_WrongMediaType(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	h := newWithRunner(&execRecorder{})
	signed := descruntime.Signature{
		Name: "my-sig",
		Signature: descruntime.SignatureInfo{
			MediaType: "application/pgp-signature",
		},
	}

	_, err := h.GetVerifyingCredentialConsumerIdentity(t.Context(), signed, nil)
	r.Error(err)
	r.Contains(err.Error(), "unsupported media type")
}

// --- ExtractCertInfoFromBundleJSON Tests ---

func TestExtractCertInfoFromBundleJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		input            func(t *testing.T) []byte
		expectedIssuer   string
		expectedIdentity string
		errContains      string
	}{
		{
			name:        "empty bundle",
			input:       func(_ *testing.T) []byte { return []byte(`{}`) },
			errContains: "bundle contains no certificate",
		},
		{
			name: "no cert in bundle",
			input: func(_ *testing.T) []byte {
				return []byte(`{"verificationMaterial":{"certificate":{"rawBytes":""}}}`)
			},
			errContains: "bundle contains no certificate",
		},
		{
			name:             "valid v1 issuer OID",
			input:            func(t *testing.T) []byte { return fakeBundleJSONWithCert(t, "https://issuer.example.com") },
			expectedIssuer:   "https://issuer.example.com",
			expectedIdentity: "signer@example.com",
		},
		{
			name: "valid v2 issuer OID",
			input: func(t *testing.T) []byte {
				return fakeBundleJSONWithCertV2(t, "https://token.actions.githubusercontent.com")
			},
			expectedIssuer: "https://token.actions.githubusercontent.com",
		},
		{
			name: "v1 fallback when v2 is malformed",
			input: func(t *testing.T) []byte {
				key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				require.NoError(t, err)
				template := &x509.Certificate{
					SerialNumber:          big.NewInt(1),
					Subject:               pkix.Name{CommonName: "test"},
					NotBefore:             time.Now().Add(-time.Hour),
					NotAfter:              time.Now().Add(time.Hour),
					BasicConstraintsValid: true,
					ExtraExtensions: []pkix.Extension{
						{Id: sigstoreIssuerV2OID, Value: []byte("not-valid-asn1")},
						{Id: sigstoreIssuerV1OID, Value: []byte("https://fallback.example.com")},
					},
				}
				certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
				require.NoError(t, err)
				certB64 := base64.StdEncoding.EncodeToString(certDER)
				bundle := map[string]any{
					"verificationMaterial": map[string]any{
						"certificate": map[string]string{"rawBytes": certB64},
					},
				}
				data, err := json.Marshal(bundle)
				require.NoError(t, err)
				return data
			},
			expectedIssuer: "https://fallback.example.com",
		},
		{
			name: "malformed v2 with no v1 surfaces error",
			input: func(t *testing.T) []byte {
				key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				require.NoError(t, err)
				template := &x509.Certificate{
					SerialNumber:          big.NewInt(1),
					Subject:               pkix.Name{CommonName: "test"},
					NotBefore:             time.Now().Add(-time.Hour),
					NotAfter:              time.Now().Add(time.Hour),
					BasicConstraintsValid: true,
					ExtraExtensions: []pkix.Extension{
						{Id: sigstoreIssuerV2OID, Value: []byte("not-valid-asn1")},
					},
				}
				certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
				require.NoError(t, err)
				certB64 := base64.StdEncoding.EncodeToString(certDER)
				bundle := map[string]any{
					"verificationMaterial": map[string]any{
						"certificate": map[string]string{"rawBytes": certB64},
					},
				}
				data, err := json.Marshal(bundle)
				require.NoError(t, err)
				return data
			},
			errContains: "V2 issuer extension",
		},
		{
			name:        "invalid JSON",
			input:       func(_ *testing.T) []byte { return []byte("not json") },
			errContains: "unmarshal bundle JSON",
		},
		{
			name: "malformed base64 certificate",
			input: func(_ *testing.T) []byte {
				return []byte(`{"verificationMaterial":{"certificate":{"rawBytes":"!!!not-base64!!!"}}}`)
			},
			errContains: "decode certificate base64",
		},
		{
			name: "malformed DER certificate",
			input: func(_ *testing.T) []byte {
				return []byte(`{"verificationMaterial":{"certificate":{"rawBytes":"` + base64.StdEncoding.EncodeToString([]byte("not-a-certificate")) + `"}}}`)
			},
			errContains: "parse Fulcio certificate",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			got, err := extractCertInfoFromBundleJSON(tc.input(t))
			if tc.errContains != "" {
				r.ErrorContains(err, tc.errContains)
				return
			}
			r.NoError(err)
			r.Equal(tc.expectedIssuer, got.Issuer)
			if tc.expectedIdentity != "" {
				r.Equal(tc.expectedIdentity, got.Identity)
			}
		})
	}
}

func TestWithOperationTimeout_DeadlineExceeded(t *testing.T) {
	t.Parallel()

	h := New(
		WithLookPath(func(string) (string, error) { return "/bin/sleep", nil }),
		WithOperationTimeout(time.Nanosecond),
	)

	_, err := h.Sign(
		t.Context(),
		testDigest(),
		testSignConfig(),
		map[string]string{CredentialKeyOIDCToken: "tok"},
	)
	require.ErrorContains(t, err, "timed out")
}
