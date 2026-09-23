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
