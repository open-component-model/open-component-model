package repository

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
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
