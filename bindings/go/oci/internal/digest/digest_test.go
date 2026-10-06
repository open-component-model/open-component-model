package digest

import (
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

func TestApply(t *testing.T) {
	for _, normalization := range []string{GenericBlobDigestV1, OCIArtifactDigestV1} {
		t.Run(normalization, func(t *testing.T) {
			r := require.New(t)
			dig := godigest.FromString("content")
			var got runtime.Digest
			r.NoError(Apply(&got, dig, normalization))
			r.Equal(runtime.Digest{HashAlgorithm: HashAlgorithmSHA256, NormalisationAlgorithm: normalization, Value: dig.Encoded()}, got)
		})
	}
}

func TestVerifyNormalizationBoundary(t *testing.T) {
	root := godigest.FromString("manifest")
	for _, tc := range []struct {
		name          string
		normalization string
		ociAllowed    bool
		blobAllowed   bool
	}{
		{name: "OCI artifact", normalization: OCIArtifactDigestV1, ociAllowed: true},
		{name: "legacy OCI label", normalization: GenericBlobDigestV1, ociAllowed: true, blobAllowed: true},
		{name: "other normalization", normalization: "jsonNormalisation/v1"},
		{name: "missing normalization"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			dig := &runtime.Digest{HashAlgorithm: HashAlgorithmSHA256, NormalisationAlgorithm: tc.normalization, Value: root.Encoded()}
			before := *dig
			if tc.ociAllowed {
				r.NoError(VerifyOCIArtifact(dig, root))
			} else {
				r.Error(VerifyOCIArtifact(dig, root))
			}
			if tc.blobAllowed {
				r.NoError(Verify(dig, root, GenericBlobDigestV1))
			} else {
				r.Error(Verify(dig, root, GenericBlobDigestV1))
			}
			// Even the legacy label must never make a manifest hash verify archive bytes.
			r.Error(Verify(dig, godigest.FromString("archive bytes"), GenericBlobDigestV1))
			r.Equal(before, *dig)
		})
	}
}

func TestVerifyOCIArtifactRejectsInvalidDigest(t *testing.T) {
	for _, normalization := range []string{OCIArtifactDigestV1, GenericBlobDigestV1} {
		t.Run(normalization, func(t *testing.T) {
			r := require.New(t)
			root := godigest.FromString("manifest")
			dig := &runtime.Digest{HashAlgorithm: HashAlgorithmSHA256, NormalisationAlgorithm: normalization, Value: root.Encoded()}
			r.ErrorContains(VerifyOCIArtifact(dig, godigest.FromString("other manifest")), "digest value mismatch")
			dig.HashAlgorithm = "SHA-512"
			r.ErrorContains(VerifyOCIArtifact(dig, root), "hash algorithm mismatch")
			r.Error(VerifyOCIArtifact(nil, root))
		})
	}
}

func TestComplete(t *testing.T) {
	root := godigest.FromString("manifest")
	for _, tc := range []struct {
		name          string
		target        runtime.Digest
		normalization string
		want          runtime.Digest
		wantError     string
	}{
		{
			name: "empty", normalization: OCIArtifactDigestV1,
			want: runtime.Digest{HashAlgorithm: HashAlgorithmSHA256, NormalisationAlgorithm: OCIArtifactDigestV1, Value: root.Encoded()},
		},
		{
			name: "matching partial", target: runtime.Digest{Value: root.Encoded()}, normalization: GenericBlobDigestV1,
			want: runtime.Digest{HashAlgorithm: HashAlgorithmSHA256, NormalisationAlgorithm: GenericBlobDigestV1, Value: root.Encoded()},
		},
		{
			name: "OCI hash spelling", target: runtime.Digest{HashAlgorithm: "sha256", Value: root.Encoded()}, normalization: OCIArtifactDigestV1,
			want: runtime.Digest{HashAlgorithm: HashAlgorithmSHA256, NormalisationAlgorithm: OCIArtifactDigestV1, Value: root.Encoded()},
		},
		{
			name: "partial legacy label is corrected", target: runtime.Digest{NormalisationAlgorithm: GenericBlobDigestV1}, normalization: OCIArtifactDigestV1,
			want: runtime.Digest{HashAlgorithm: HashAlgorithmSHA256, NormalisationAlgorithm: OCIArtifactDigestV1, Value: root.Encoded()},
		},
		{
			name: "OCI label on blob", target: runtime.Digest{NormalisationAlgorithm: OCIArtifactDigestV1}, normalization: GenericBlobDigestV1,
			wantError: "normalisation algorithm mismatch",
		},
		{
			name: "mismatching value", target: runtime.Digest{HashAlgorithm: HashAlgorithmSHA256, Value: godigest.FromString("other").Encoded()}, normalization: OCIArtifactDigestV1,
			wantError: "digest value mismatch",
		},
		{
			name: "mismatching hash", target: runtime.Digest{HashAlgorithm: HashAlgorithmSHA512}, normalization: OCIArtifactDigestV1,
			wantError: "hash algorithm mismatch",
		},
		{
			name: "unsupported normalization", normalization: "jsonNormalisation/v1",
			wantError: "unsupported normalisation algorithm",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			got := tc.target
			err := Complete(&got, root, tc.normalization)
			if tc.wantError != "" {
				r.ErrorContains(err, tc.wantError)
				r.Equal(tc.target, got, "a rejected digest must not be rewritten")
				return
			}
			r.NoError(err)
			r.Equal(tc.want, got)
		})
	}
	require.Error(t, Complete(nil, root, OCIArtifactDigestV1))
}
