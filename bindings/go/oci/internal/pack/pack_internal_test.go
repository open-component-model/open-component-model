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
		unlabelled := &descriptor.Digest{HashAlgorithm: "SHA-256", Value: root.Digest.Encoded()}
		// An unlabelled digest describes the bytes of a blob: the blob rule verifies it and
		// keeps it as recorded, while an OCI artifact digest cannot be checked against bytes.
		unlabelledWant, unlabelledErr := unlabelled, ""
		if normalization == descriptor.OCIArtifactDigestV1 {
			unlabelledWant, unlabelledErr = nil, "normalisation algorithm mismatch"
		}
		for _, tc := range []struct {
			name    string
			digest  *descriptor.Digest
			want    *descriptor.Digest // nil => the digest must be rejected, not inferred
			wantErr string
		}{
			{name: "absent is computed", digest: nil, want: computed(normalization)},
			{name: "empty is computed", digest: &descriptor.Digest{}, want: computed(normalization)},
			{name: "complete is preserved", digest: computed(normalization), want: computed(normalization)},
			{name: "missing normalisation", digest: unlabelled, want: unlabelledWant, wantErr: unlabelledErr},
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
					wantErr := tc.wantErr
					if wantErr == "" {
						wantErr = "a digest must be complete or absent"
					}
					r.ErrorContains(err, wantErr)
					r.Equal(before, resource.Digest, "a rejected digest must not be rewritten")
					return
				}
				r.NoError(err)
				r.Equal(tc.want, resource.Digest)
			})
		}
	}
}
