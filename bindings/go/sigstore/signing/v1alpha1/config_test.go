package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSignConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     SignConfig
		wantErr string
	}{
		{"valid empty", SignConfig{}, ""},
		{"valid signingConfig only", SignConfig{SigningConfig: "/path/to/config.json"}, ""},
		{"valid with issuer and clientID", SignConfig{Issuer: "https://keycloak.corp.example.com/realms/sigstore", ClientID: "corp-sigstore"}, ""},
		{"valid issuer only", SignConfig{Issuer: "https://dex.example.com"}, ""},
		{"valid http issuer accepted", SignConfig{Issuer: "http://dex.example.com"}, ""},
		{"invalid issuer URL", SignConfig{Issuer: "not-a-url"}, "Issuer"},
		{"invalid issuer not absolute", SignConfig{Issuer: "//dex.example.com"}, "must be absolute"},
		{"invalid issuer with query", SignConfig{Issuer: "https://dex.example.com?foo=bar"}, "must not contain a query"},
		{"invalid issuer with fragment", SignConfig{Issuer: "https://dex.example.com#frag"}, "must not contain a fragment"},
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
