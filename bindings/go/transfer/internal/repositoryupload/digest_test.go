package repositoryupload

import (
	"bytes"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// A resource excluded from signing (ocm-spec: NO-DIGEST / EXCLUDE-FROM-SIGNATURE) is uploaded
// without a content check and published with its digest unchanged. Mirrors Upload's
// knownDigest -> Put -> uploadedDigest sequence.
func TestUploadDigestExcludedFromSignature(t *testing.T) {
	content := []byte("content that must not be checked")
	tests := []struct {
		name    string
		fromOCI bool
	}{
		{name: "plain blob source"},
		{name: "OCI artifact source", fromOCI: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			src := descriptor.NewExcludeFromSignatureDigest()

			expected, _, err := knownDigest(src, inmemory.New(bytes.NewReader(content)), tt.fromOCI)
			r.NoError(err)
			r.Empty(expected, "an excluded digest must not be expected of the uploaded bytes")

			r.Equal(descriptor.NewExcludeFromSignatureDigest(), uploadedDigest(src, expected, digest.FromBytes(content)))
		})
	}
}

// Uploading an OCI artifact stores it in another byte representation, so a recorded digest
// cannot be kept and the upload fails before any content is written.
func TestUploadDigestFromOCIFails(t *testing.T) {
	r := require.New(t)
	src := &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "ociArtifactDigest/v1", Value: digest.FromString("manifest").Encoded()}

	_, _, err := knownDigest(src, inmemory.New(bytes.NewReader([]byte("layout"))), true)
	r.ErrorContains(err, "recorded ociArtifactDigest/v1 digest cannot be kept")
}
