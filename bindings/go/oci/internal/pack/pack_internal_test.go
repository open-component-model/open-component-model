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

func TestUpdateArtifactAccess_DigestPolicy(t *testing.T) {
	root := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, []byte("test content"))
	computed := func(normalization string) *descriptor.Digest {
		return &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: normalization, Value: root.Digest.Encoded()}
	}
	for _, normalization := range []string{"genericBlobDigest/v1", "ociArtifactDigest/v1"} {
		for _, tc := range []struct {
			name   string
			digest *descriptor.Digest
			want   *descriptor.Digest // nil => the partial digest must be rejected, not inferred
		}{
			{name: "absent is computed", digest: nil, want: computed(normalization)},
			{name: "empty is computed", digest: &descriptor.Digest{}, want: computed(normalization)},
			{name: "complete is preserved", digest: computed(normalization), want: computed(normalization)},
			{name: "missing normalisation rejected", digest: &descriptor.Digest{HashAlgorithm: "SHA-256", Value: root.Digest.Encoded()}},
			{name: "missing hash rejected", digest: &descriptor.Digest{NormalisationAlgorithm: normalization, Value: root.Digest.Encoded()}},
			{name: "missing value rejected", digest: &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: normalization}},
			{name: "value only rejected", digest: &descriptor.Digest{Value: root.Digest.Encoded()}},
		} {
			t.Run(normalization+"/"+tc.name, func(t *testing.T) {
				r := require.New(t)
				scheme := runtime.NewScheme()
				v2.MustAddToScheme(scheme)
				resource := &descriptor.Resource{}
				var before *descriptor.Digest
				if tc.digest != nil {
					resource.Digest = tc.digest.DeepCopy()
					before = tc.digest.DeepCopy()
				}

				err := updateArtifactAccess(resource, &v2.LocalBlob{}, root, updateAccessOptions{
					Options: Options{AccessScheme: scheme}, NormalisationAlgorithm: normalization,
				})
				if tc.want == nil {
					r.ErrorContains(err, "a digest must be complete or absent")
					r.Equal(before, resource.Digest, "a rejected digest must not be rewritten")
					return
				}
				r.NoError(err)
				r.Equal(tc.want, resource.Digest)
			})
		}
	}
}
