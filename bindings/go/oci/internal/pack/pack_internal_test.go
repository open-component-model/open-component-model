package pack

import (
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestUpdateArtifactAccess_ReplacesPartialDigest(t *testing.T) {
	for _, tc := range []struct {
		name   string
		digest descriptor.Digest
	}{
		{name: "missing normalization", digest: descriptor.Digest{HashAlgorithm: "SHA-256", Value: "previous"}},
		{name: "missing hash", digest: descriptor.Digest{NormalisationAlgorithm: "ociArtifactDigest/v1", Value: "previous"}},
		{name: "missing value", digest: descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "ociArtifactDigest/v1"}},
		{name: "normalisation only", digest: descriptor.Digest{NormalisationAlgorithm: "ociArtifactDigest/v1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			v2.MustAddToScheme(scheme)
			resource := &descriptor.Resource{Digest: tc.digest.DeepCopy()}
			root := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, []byte("test content"))

			for _, normalization := range []string{"genericBlobDigest/v1", "ociArtifactDigest/v1"} {
				t.Run(normalization, func(t *testing.T) {
					r := require.New(t)
					resource.Digest = tc.digest.DeepCopy()
					r.NoError(updateArtifactAccess(resource, &v2.LocalBlob{}, root, updateAccessOptions{
						Options: Options{AccessScheme: scheme}, NormalisationAlgorithm: normalization,
					}))
					r.Equal(&descriptor.Digest{
						HashAlgorithm:          "SHA-256",
						NormalisationAlgorithm: normalization,
						Value:                  root.Digest.Encoded(),
					}, resource.Digest)
				})
			}
		})
	}
}
