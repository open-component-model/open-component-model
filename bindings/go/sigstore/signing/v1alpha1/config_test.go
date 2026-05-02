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
		{"valid https URLs", SignConfig{FulcioURL: "https://fulcio.example.com", RekorURL: "https://rekor.example.com"}, ""},
		{"trustedRoot without endpoints rejected", SignConfig{TrustedRoot: "/path/to/root.json"}, "no signing infrastructure is configured"},
		{"trustedRoot with signingConfig accepted", SignConfig{TrustedRoot: "/path/to/root.json", SigningConfig: "/path/to/config.json"}, ""},
		{"trustedRoot with explicit URL accepted", SignConfig{TrustedRoot: "/path/to/root.json", FulcioURL: "https://fulcio.example.com"}, ""},
		{"http FulcioURL rejected", SignConfig{FulcioURL: "http://fulcio.example.com"}, "must use https scheme"},
		{"http RekorURL rejected", SignConfig{RekorURL: "http://rekor.example.com"}, "must use https scheme"},
		{"http TimestampServerURL rejected", SignConfig{TimestampServerURL: "http://tsa.example.com"}, "must use https scheme"},
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
		{"http issuer rejected", VerifyConfig{CertificateOIDCIssuer: "http://accounts.google.com", CertificateIdentity: "user@example.com"}, "must use https scheme"},
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
