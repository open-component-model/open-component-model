package handler

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"

	"ocm.software/open-component-model/bindings/go/sigstore/signing/v1alpha1"
)

func TestDoVerify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		cfgSetup  func(cfg *v1alpha1.VerifyConfig)
		creds     map[string]string
		assertErr require.ErrorAssertionFunc
		assert    func(t *testing.T, mock *mockExecutor)
	}{
		{
			name:      "exact issuer and identity",
			assertErr: require.NoError,
			assert: func(t *testing.T, mock *mockExecutor) {
				r := require.New(t)
				r.True(mock.verifyCalled)
				r.Equal("user@example.com", mock.verifyOpts.CertificateIdentity)
				r.Equal("https://accounts.google.com", mock.verifyOpts.CertificateOIDCIssuer)
				r.Empty(mock.verifyOpts.CertificateIdentityRegexp)
				r.Empty(mock.verifyOpts.CertificateOIDCIssuerRegexp)
			},
		},
		{
			name: "regexp issuer and identity",
			cfgSetup: func(cfg *v1alpha1.VerifyConfig) {
				cfg.CertificateOIDCIssuer = ""
				cfg.CertificateIdentity = ""
				cfg.CertificateOIDCIssuerRegexp = ".*google.*"
				cfg.CertificateIdentityRegexp = ".*@example.com"
				cfg.TrustedRoot = "/path/to/trusted_root.json"
			},
			assertErr: require.NoError,
			assert: func(t *testing.T, mock *mockExecutor) {
				r := require.New(t)
				r.True(mock.verifyCalled)
				r.Empty(mock.verifyOpts.CertificateIdentity)
				r.Empty(mock.verifyOpts.CertificateOIDCIssuer)
				r.Equal(".*@example.com", mock.verifyOpts.CertificateIdentityRegexp)
				r.Equal(".*google.*", mock.verifyOpts.CertificateOIDCIssuerRegexp)
				r.Equal("/path/to/trusted_root.json", mock.verifyOpts.TrustedRoot)
			},
		},
		{
			name: "private infrastructure",
			cfgSetup: func(cfg *v1alpha1.VerifyConfig) {
				cfg.TrustedRoot = "/path/to/private_trusted_root.json"
				cfg.PrivateInfrastructure = true
			},
			assertErr: require.NoError,
			assert: func(t *testing.T, mock *mockExecutor) {
				r := require.New(t)
				r.True(mock.verifyCalled)
				r.True(mock.verifyOpts.PrivateInfrastructure)
				r.Equal("/path/to/private_trusted_root.json", mock.verifyOpts.TrustedRoot)
			},
		},
		{
			name: "trusted root from inline JSON credential",
			creds: map[string]string{
				// Minimal stub -- the mock executor doesn't validate trusted root content.
				CredentialKeyTrustedRootJSON: `{"mediaType":"application/vnd.dev.sigstore.trustedroot+json;version=0.1"}`,
			},
			assertErr: require.NoError,
			assert: func(t *testing.T, mock *mockExecutor) {
				r := require.New(t)
				r.True(mock.verifyCalled)
				r.NotEmpty(mock.verifyOpts.TrustedRoot)
			},
		},
		{
			name: "trusted root from file credential",
			creds: map[string]string{
				CredentialKeyTrustedRootJSONFile: "/custom/path/trusted_root.json",
			},
			assertErr: require.NoError,
			assert: func(t *testing.T, mock *mockExecutor) {
				r := require.New(t)
				r.Equal("/custom/path/trusted_root.json", mock.verifyOpts.TrustedRoot)
			},
		},
		{
			name:      "success with default config",
			assertErr: require.NoError,
		},
		{
			name:      "no trusted root yields empty flag",
			assertErr: require.NoError,
			assert: func(t *testing.T, mock *mockExecutor) {
				r := require.New(t)
				r.Empty(mock.verifyOpts.TrustedRoot)
			},
		},
		{
			name:      "passes digest bytes to executor",
			assertErr: require.NoError,
			assert: func(t *testing.T, mock *mockExecutor) {
				r := require.New(t)
				expectedBytes, err := hex.DecodeString(testDigest().Value)
				r.NoError(err)
				r.Equal(expectedBytes, mock.verifyData)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mock := &mockExecutor{}
			h := NewWithExecutor(mock)

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
			tc.assertErr(t, err)

			if tc.assert != nil {
				tc.assert(t, mock)
			}
		})
	}
}

func TestVerify_Failure(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	mock := &mockExecutor{
		verifyErr: fmt.Errorf("cosign verify-blob failed: exit status 1\nstderr: signature verification failed"),
	}
	h := NewWithExecutor(mock)

	cfg := testVerifyConfig()
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
	r.Contains(err.Error(), "verify signature")
}

func TestVerify_MissingIdentity(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	h := NewWithExecutor(&mockExecutor{})

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
	r.Contains(err.Error(), "keyless verification requires both an issuer constraint and an identity constraint")
}

func TestVerify_PrivateInfrastructureWithoutTrustedRoot(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	h := NewWithExecutor(&mockExecutor{})

	cfg := testVerifyConfig()
	cfg.PrivateInfrastructure = true
	// no TrustedRoot set

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

func TestVerify_PrivateInfrastructureWithTrustedRootCredential(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	mock := &mockExecutor{}
	h := NewWithExecutor(mock)

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
	r.True(mock.verifyCalled)
	r.True(mock.verifyOpts.PrivateInfrastructure)
}

func TestVerify_CertificateOIDCIssuerRejectsHTTP(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	h := NewWithExecutor(&mockExecutor{})

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
	r.Error(err)
	r.Contains(err.Error(), "must use https scheme")
}

func TestHasIdentityConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  *v1alpha1.VerifyConfig
		want bool
	}{
		{
			name: "empty config -- nothing set",
			cfg:  &v1alpha1.VerifyConfig{},
			want: false,
		},
		{
			name: "only issuer set -- missing identity",
			cfg:  &v1alpha1.VerifyConfig{CertificateOIDCIssuer: "https://accounts.google.com"},
			want: false,
		},
		{
			name: "only identity set -- missing issuer",
			cfg:  &v1alpha1.VerifyConfig{CertificateIdentity: "user@example.com"},
			want: false,
		},
		{
			name: "only issuer regexp set -- missing identity",
			cfg:  &v1alpha1.VerifyConfig{CertificateOIDCIssuerRegexp: "https://.*"},
			want: false,
		},
		{
			name: "only identity regexp set -- missing issuer",
			cfg:  &v1alpha1.VerifyConfig{CertificateIdentityRegexp: ".*@example.com"},
			want: false,
		},
		{
			name: "issuer + identity both set",
			cfg: &v1alpha1.VerifyConfig{
				CertificateOIDCIssuer: "https://accounts.google.com",
				CertificateIdentity:   "user@example.com",
			},
			want: true,
		},
		{
			name: "issuer regexp + identity regexp both set",
			cfg: &v1alpha1.VerifyConfig{
				CertificateOIDCIssuerRegexp: "https://.*",
				CertificateIdentityRegexp:   ".*@example.com",
			},
			want: true,
		},
		{
			name: "issuer + identity regexp both set",
			cfg: &v1alpha1.VerifyConfig{
				CertificateOIDCIssuer:     "https://accounts.google.com",
				CertificateIdentityRegexp: ".*@example.com",
			},
			want: true,
		},
		{
			name: "issuer regexp + identity both set",
			cfg: &v1alpha1.VerifyConfig{
				CertificateOIDCIssuerRegexp: "https://.*",
				CertificateIdentity:         "user@example.com",
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := hasIdentityConfig(tc.cfg)
			if got != tc.want {
				t.Errorf("hasIdentityConfig() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestVerify_InvalidBase64Bundle(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	h := NewWithExecutor(&mockExecutor{})

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

	h := NewWithExecutor(&mockExecutor{})

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

	h := NewWithExecutor(&mockExecutor{})
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
