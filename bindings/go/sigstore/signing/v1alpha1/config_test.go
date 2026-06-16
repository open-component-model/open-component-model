package v1alpha1

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSignConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		cfg       SignConfig
		wantErr   string
		wantErrIs error
	}{
		{name: "valid empty", cfg: SignConfig{}},
		{name: "valid signingConfig only", cfg: SignConfig{SigningConfig: "/path/to/config.json"}},
		{name: "valid with issuer and clientID", cfg: SignConfig{Issuer: "https://keycloak.corp.example.com/realms/sigstore", ClientID: "corp-sigstore"}},
		{name: "valid issuer only", cfg: SignConfig{Issuer: "https://dex.example.com"}},
		{name: "valid http issuer accepted", cfg: SignConfig{Issuer: "http://dex.example.com"}},
		{name: "invalid issuer URL", cfg: SignConfig{Issuer: "not-a-url"}, wantErr: "Issuer"},
		{name: "invalid issuer not absolute", cfg: SignConfig{Issuer: "//dex.example.com"}, wantErr: "must be absolute"},
		{name: "invalid issuer with query", cfg: SignConfig{Issuer: "https://dex.example.com?foo=bar"}, wantErr: "must not contain a query"},
		{name: "invalid issuer with fragment", cfg: SignConfig{Issuer: "https://dex.example.com#frag"}, wantErr: "must not contain a fragment"},
		{name: "valid known SignatureAlgorithm", cfg: SignConfig{SignatureAlgorithm: AlgorithmSigstoreV1Alpha1}},
		{name: "invalid unknown SignatureAlgorithm", cfg: SignConfig{SignatureAlgorithm: "Sigstore/v99alpha1"}, wantErrIs: ErrUnknownAlgorithm},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			err := tc.cfg.Validate()
			switch {
			case tc.wantErrIs != nil:
				r.Error(err)
				r.True(errors.Is(err, tc.wantErrIs), "expected %v, got %v", tc.wantErrIs, err)
			case tc.wantErr != "":
				r.ErrorContains(err, tc.wantErr)
			default:
				r.NoError(err)
			}
		})
	}
}

func TestSignConfig_GetSignatureAlgorithm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  *SignConfig
		want SignatureAlgorithm
	}{
		{"empty field returns default", &SignConfig{}, AlgorithmSigstoreDefault},
		{"explicit value passes through", &SignConfig{SignatureAlgorithm: AlgorithmSigstoreV1Alpha1}, AlgorithmSigstoreV1Alpha1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, tc.cfg.GetSignatureAlgorithm())
		})
	}
}

func TestVerifyConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     VerifyConfig
		wantErr string
	}{
		{"missing identity constraints", VerifyConfig{}, "keyless verification requires"},
		{"issuer only missing identity", VerifyConfig{CertificateOIDCIssuer: "https://accounts.google.com"}, "keyless verification requires"},
		{"identity only missing issuer", VerifyConfig{CertificateIdentity: "user@example.com"}, "keyless verification requires"},
		{"valid exact issuer + exact identity", VerifyConfig{CertificateOIDCIssuer: "https://accounts.google.com", CertificateIdentity: "user@example.com"}, ""},
		{"valid regexp issuer + regexp identity", VerifyConfig{CertificateOIDCIssuerRegexp: "https://.*", CertificateIdentityRegexp: ".*@example.com"}, ""},
		{"valid issuer + identity regexp", VerifyConfig{CertificateOIDCIssuer: "https://accounts.google.com", CertificateIdentityRegexp: ".*@example.com"}, ""},
		{"valid issuer regexp + identity", VerifyConfig{CertificateOIDCIssuerRegexp: "https://.*", CertificateIdentity: "user@example.com"}, ""},
		{"http issuer accepted", VerifyConfig{CertificateOIDCIssuer: "http://accounts.google.com", CertificateIdentity: "user@example.com"}, ""},
		{"invalid issuer not absolute", VerifyConfig{CertificateOIDCIssuer: "//accounts.google.com", CertificateIdentity: "user@example.com"}, "must be absolute"},
		{"invalid issuer with query", VerifyConfig{CertificateOIDCIssuer: "https://accounts.google.com?x=1", CertificateIdentity: "user@example.com"}, "must not contain a query"},
		{"invalid issuer with fragment", VerifyConfig{CertificateOIDCIssuer: "https://accounts.google.com#frag", CertificateIdentity: "user@example.com"}, "must not contain a fragment"},
		{"invalid issuer regexp", VerifyConfig{CertificateOIDCIssuerRegexp: "[invalid", CertificateIdentity: "user@example.com"}, "invalid regexp"},
		{"invalid identity regexp", VerifyConfig{CertificateOIDCIssuer: "https://accounts.google.com", CertificateIdentityRegexp: "(unclosed"}, "invalid regexp"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			err := tc.cfg.Validate()
			if tc.wantErr == "" {
				r.NoError(err)
			} else {
				r.ErrorContains(err, tc.wantErr)
			}
		})
	}
}
