package pack

import (
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	"oras.land/oras-go/v2/content"
	"testing"
)

func TestUpdateArtifactAccess_ReplacesPartialDigest(t *testing.T) {
	for _, tc := range []struct {
		name   string
		digest descriptor.Digest
	}{
		{name: "missing hash", digest: descriptor.Digest{NormalisationAlgorithm: "ociArtifactDigest/v1", Value: "previous"}},
		{name: "missing value", digest: descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "ociArtifactDigest/v1"}},
		{name: "normalisation only", digest: descriptor.Digest{NormalisationAlgorithm: "ociArtifactDigest/v1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			scheme := runtime.NewScheme()
			v2.MustAddToScheme(scheme)
			resource := &descriptor.Resource{Digest: tc.digest.DeepCopy()}
			root := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, []byte("test content"))

			r.NoError(updateArtifactAccess(resource, &v2.LocalBlob{}, root, updateAccessOptions{
				Options: Options{AccessScheme: scheme},
			}))
			r.Equal(&descriptor.Digest{
				HashAlgorithm:          "SHA-256",
				NormalisationAlgorithm: "genericBlobDigest/v1",
				Value:                  root.Digest.Encoded(),
			}, resource.Digest)
		})
	}
}
