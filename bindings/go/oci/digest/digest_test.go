package digest_test

import (
	"crypto"
	"strings"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocmdigest "ocm.software/open-component-model/bindings/go/digest"
	ocidigest "ocm.software/open-component-model/bindings/go/oci/digest"
)

func TestFromOCI(t *testing.T) {
	for _, tc := range []struct {
		name    string
		digest  godigest.Digest
		want    crypto.Hash
		wantErr string
	}{
		{name: "sha256", digest: godigest.SHA256.FromString("manifest"), want: crypto.SHA256},
		{name: "sha512", digest: godigest.SHA512.FromString("manifest"), want: crypto.SHA512},
		{name: "sha384", digest: godigest.NewDigestFromEncoded("sha384", strings.Repeat("a", 96)), wantErr: "sha384"},
		{name: "invalid", digest: "sha256:abc", wantErr: "invalid OCI digest"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			got, err := ocidigest.FromOCI(tc.digest)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(ocmdigest.Computed{Hash: tc.want, Value: tc.digest.Encoded()}, got)
		})
	}
}

// A manifest digest recorded with the generic label by earlier v2 releases verifies against the
// manifest and keeps its label; the blob rule never accepts the OCI label.
func TestRules(t *testing.T) {
	r := require.New(t)
	computed, err := ocidigest.FromOCI(godigest.FromString("manifest"))
	r.NoError(err)

	for _, label := range []string{ocmdigest.GenericBlobDigestV1, ocidigest.LegacyArtifactDigestV1} {
		legacy := &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: label, Value: computed.Value}
		res, err := ocmdigest.Check(legacy, computed, ocidigest.ArtifactRule)
		r.NoError(err, label)
		r.False(res.Generated, label)
		r.Equal(legacy, res.Digest, label)
	}

	artifact := &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: ocidigest.ArtifactDigestV1, Value: computed.Value}
	_, err = ocmdigest.Check(artifact, computed, ocidigest.BlobRule)
	r.ErrorContains(err, "normalisation algorithm mismatch")
}
