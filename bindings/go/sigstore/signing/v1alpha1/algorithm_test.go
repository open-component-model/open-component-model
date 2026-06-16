package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsKnownAlgorithm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		alg  SignatureAlgorithm
		want bool
	}{
		{"v1alpha1 is known", AlgorithmSigstoreV1Alpha1, true},
		{"default alias is known", AlgorithmSigstoreDefault, true},
		{"empty is not known", "", false},
		{"future version is not known", "Sigstore/v2alpha1", false},
		{"unrelated value is not known", "RSASSA-PSS", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, IsKnownAlgorithm(tc.alg))
		})
	}
}

func TestIsAcceptableMediaType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mt   string
		want bool
	}{
		{"v0.3 bundle is accepted", MediaTypeSigstoreBundle, true},
		{"empty is not accepted", "", false},
		{"future bundle version is not accepted", "application/vnd.dev.sigstore.bundle.v0.4+json", false},
		{"unrelated media type is not accepted", "application/pgp-signature", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, IsAcceptableMediaType(tc.mt))
		})
	}
}
