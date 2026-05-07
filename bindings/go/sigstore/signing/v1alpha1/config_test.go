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
