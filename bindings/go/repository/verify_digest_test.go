package repository

import (
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

func TestDigest_Parse(t *testing.T) {
	value := digest.FromString("content").Encoded()

	tests := []struct {
		name     string
		digest   *descruntime.Digest
		expected digest.Digest
		noDigest bool
	}{
		{
			name:     "sha-256 as the spec spells it",
			digest:   &descruntime.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: value},
			expected: digest.NewDigestFromEncoded(digest.SHA256, value),
		},
		{
			name:     "sha256 without separator",
			digest:   &descruntime.Digest{HashAlgorithm: "sha256", Value: value},
			expected: digest.NewDigestFromEncoded(digest.SHA256, value),
		},
		{
			name:     "uppercase value is folded",
			digest:   &descruntime.Digest{HashAlgorithm: "SHA-256", Value: strings.ToUpper(value)},
			expected: digest.NewDigestFromEncoded(digest.SHA256, value),
		},
		{
			name:   "sha-512 is not supported",
			digest: &descruntime.Digest{HashAlgorithm: "SHA-512", Value: digest.SHA512.FromString("content").Encoded()},
		},
		{
			name:     "nil digest",
			digest:   nil,
			noDigest: true,
		},
		{
			name:     "no fields set at all",
			digest:   &descruntime.Digest{},
			noDigest: true,
		},
		{
			name:   "value without hash algorithm",
			digest: &descruntime.Digest{Value: value},
		},
		{
			name:   "hash algorithm without value",
			digest: &descruntime.Digest{HashAlgorithm: "SHA-256"},
		},
		{
			name:     "no-digest without a value is still an opt-out",
			digest:   &descruntime.Digest{HashAlgorithm: descruntime.NoDigest},
			noDigest: true,
		},
		{
			name:     "excluded from signature",
			digest:   &descruntime.Digest{HashAlgorithm: descruntime.NoDigest, NormalisationAlgorithm: descruntime.ExcludeFromSignature, Value: descruntime.NoDigest},
			noDigest: true,
		},
		{
			name:   "unsupported algorithm",
			digest: &descruntime.Digest{HashAlgorithm: "md5", Value: value},
		},
		{
			name:   "value that is not a digest of the algorithm",
			digest: &descruntime.Digest{HashAlgorithm: "SHA-256", Value: "nothex"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := parseDigest(tt.digest)

			switch {
			case tt.noDigest:
				require.NoError(t, err, "nothing to verify against is not a failure")
				require.Empty(t, parsed)
			case tt.expected == "":
				require.Error(t, err, "a present but unusable digest must not read as an absent one")
			default:
				require.NoError(t, err)
				require.Equal(t, tt.expected, parsed)
			}
		})
	}
}
