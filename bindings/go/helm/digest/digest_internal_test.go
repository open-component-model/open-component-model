package digest

import (
	"testing"

	ociDigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

func TestResourceDigestNormalization(t *testing.T) {
	resolved := ociDigest.FromString("helm manifest")
	for _, test := range []struct {
		name          string
		normalization string
		existing      *runtime.Digest
		want          runtime.Digest
		wantError     string
	}{
		{
			name:          "new OCI chart",
			normalization: "ociArtifactDigest/v1",
			want:          runtime.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "ociArtifactDigest/v1", Value: resolved.Encoded()},
		},
		{
			name:          "new HTTP chart",
			normalization: "genericBlobDigest/v1",
			want:          runtime.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: resolved.Encoded()},
		},
		{
			name:          "existing OCI chart",
			normalization: "ociArtifactDigest/v1",
			existing:      &runtime.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "ociArtifactDigest/v1", Value: resolved.Encoded()},
		},
		{
			name:          "historical OCI chart",
			normalization: "ociArtifactDigest/v1",
			existing:      &runtime.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: resolved.Encoded()},
		},
		{
			name:          "partial historical OCI chart",
			normalization: "ociArtifactDigest/v1",
			existing:      &runtime.Digest{NormalisationAlgorithm: "genericBlobDigest/v1", Value: resolved.Encoded()},
			want:          runtime.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "ociArtifactDigest/v1", Value: resolved.Encoded()},
		},
		{
			name:          "partial OCI hash spelling",
			normalization: "ociArtifactDigest/v1",
			existing:      &runtime.Digest{HashAlgorithm: "sha256", Value: resolved.Encoded()},
			want:          runtime.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "ociArtifactDigest/v1", Value: resolved.Encoded()},
		},
		{
			name:          "wrong OCI normalization",
			normalization: "ociArtifactDigest/v1",
			existing:      &runtime.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "jsonNormalisation/v1", Value: resolved.Encoded()},
			wantError:     "normalisation algorithm mismatch",
		},
		{
			name:          "partial OCI chart with wrong value",
			normalization: "ociArtifactDigest/v1",
			existing:      &runtime.Digest{HashAlgorithm: "SHA-256", Value: ociDigest.FromString("other manifest").Encoded()},
			wantError:     "digest value mismatch",
		},
		{
			name:          "wrong HTTP normalization",
			normalization: "genericBlobDigest/v1",
			existing:      &runtime.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "ociArtifactDigest/v1", Value: resolved.Encoded()},
			wantError:     "normalisation algorithm mismatch",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)
			if test.existing == nil {
				var got runtime.Digest
				r.NoError(applyDigest(&got, resolved, test.normalization))
				r.Equal(test.want, got)
				return
			}
			before := *test.existing
			err := verifyDigest(test.existing, resolved, test.normalization)
			if test.wantError != "" {
				r.ErrorContains(err, test.wantError)
			} else {
				r.NoError(err)
			}
			if test.want.HashAlgorithm != "" {
				r.Equal(test.want, *test.existing)
			} else {
				r.Equal(before, *test.existing)
			}
		})
	}
}
