package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfig_GetKeyFingerprint(t *testing.T) {
	const fpr = "B118BE3A32BE4AF28E37E881167C7102F8AC81E4"
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{name: "nil config", cfg: nil, want: ""},
		{name: "unset", cfg: &Config{}, want: ""},
		{name: "plain fingerprint", cfg: &Config{KeyFingerprint: fpr}, want: fpr},
		{name: "0x prefix", cfg: &Config{KeyFingerprint: "0x" + fpr}, want: fpr},
		{name: "0X prefix on a long key ID", cfg: &Config{KeyFingerprint: "0X167C7102F8AC81E4"}, want: "167C7102F8AC81E4"},
		{name: "gpg --fingerprint spacing", cfg: &Config{KeyFingerprint: " B118 BE3A 32BE 4AF2 8E37  E881 167C 7102 F8AC 81E4 "}, want: fpr},
		{name: "spaced with 0x prefix", cfg: &Config{KeyFingerprint: "0xB118 BE3A 32BE 4AF2 8E37  E881 167C 7102 F8AC 81E4"}, want: fpr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.New(t).Equal(tt.want, tt.cfg.GetKeyFingerprint())
		})
	}
}
