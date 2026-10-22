package repository

import (
	"crypto"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	jsonv1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v1"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
)

func TestParseDigestPin(t *testing.T) {
	hex256 := strings.Repeat("ab", 32)
	tests := []struct {
		pin     string
		want    *descruntime.Digest
		wantErr string
	}{
		{
			pin:  "sha256:" + hex256,
			want: &descruntime.Digest{HashAlgorithm: "SHA-256", Value: hex256},
		},
		{
			pin:  "jsonNormalisation.v4alpha1+sha512:" + strings.Repeat("cd", 64),
			want: &descruntime.Digest{HashAlgorithm: "SHA-512", NormalisationAlgorithm: "jsonNormalisation/v4alpha1", Value: strings.Repeat("cd", 64)},
		},
		{
			pin:  "SHA256:" + strings.ToUpper(hex256),
			want: &descruntime.Digest{HashAlgorithm: "SHA-256", Value: hex256},
		},
		{pin: "md5:" + strings.Repeat("ab", 16), wantErr: "unsupported hash"},
		{pin: "sha256:" + hex256[2:], wantErr: "must have 64 hex characters"},
		{pin: "+sha256:" + hex256, wantErr: "invalid digest pin"},
	}
	for _, tt := range tests {
		t.Run(tt.pin, func(t *testing.T) {
			got, err := ParseDigestPin(tt.pin)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
			require.Equal(t, strings.ToLower(tt.pin), strings.ToLower(FormatDigestPin(got)), "formatting round-trips")
		})
	}
}

func TestVerifyNormalizedPin_LegacyV3(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	desc := &descruntime.Descriptor{
		Component: descruntime.Component{
			ComponentMeta: descruntime.ComponentMeta{
				ObjectMeta: descruntime.ObjectMeta{Name: "acme.org/test", Version: "1.0.0"},
			},
			Provider: descruntime.Provider{Name: "acme"},
		},
	}

	generated, err := signing.GenerateDigest(ctx, desc, slog.Default(), v4alpha1.Algorithm, crypto.SHA256.String())
	r.NoError(err)

	v3Pin, err := ParseDigestPin("jsonNormalisation.v3+sha256:" + generated.Value)
	r.NoError(err)
	r.NoError(verifyNormalizedPin(ctx, desc, v3Pin), "a legacy v3 pin verifies like a v4alpha1 pin")

	wrong, err := ParseDigestPin("jsonNormalisation.v3+sha256:" + strings.Repeat("00", 32))
	r.NoError(err)
	r.ErrorContains(verifyNormalizedPin(ctx, desc, wrong), "does not match pinned digest")
}

// TestVerifyNormalizedPin_V1 is a regression for the verify-only guard: a
// jsonNormalisation/v1 normalized pin must verify. Read-only verification goes
// through GenerateDigest, which used to carry the signing block and wrongly
// rejected this path with "supported for verification only, not signing".
func TestVerifyNormalizedPin_V1(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	desc := &descruntime.Descriptor{
		Component: descruntime.Component{
			ComponentMeta: descruntime.ComponentMeta{
				ObjectMeta: descruntime.ObjectMeta{Name: "acme.org/test", Version: "1.0.0"},
			},
			Provider: descruntime.Provider{Name: "acme"},
		},
	}

	generated, err := signing.GenerateDigest(ctx, desc, slog.Default(), jsonv1.Algorithm, crypto.SHA256.String())
	r.NoError(err)

	v1Pin, err := ParseDigestPin("jsonNormalisation.v1+sha256:" + generated.Value)
	r.NoError(err)
	r.NoError(verifyNormalizedPin(ctx, desc, v1Pin), "a v1 pin must verify (v1 is verify-only, not sign-only)")

	wrong, err := ParseDigestPin("jsonNormalisation.v1+sha256:" + strings.Repeat("00", 32))
	r.NoError(err)
	r.ErrorContains(verifyNormalizedPin(ctx, desc, wrong), "does not match pinned digest")
}
