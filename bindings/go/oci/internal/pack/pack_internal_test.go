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

func TestUpdateArtifactAccess_CompletesPartialDigest(t *testing.T) {
	root := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, []byte("test content"))
	for _, normalization := range []string{"genericBlobDigest/v1", "ociArtifactDigest/v1"} {
		for _, tc := range []struct {
			name      string
			digest    descriptor.Digest
			wantError string
		}{
			{name: "missing normalization", digest: descriptor.Digest{HashAlgorithm: "SHA-256", Value: root.Digest.Encoded()}},
			{name: "missing hash", digest: descriptor.Digest{NormalisationAlgorithm: normalization, Value: root.Digest.Encoded()}},
			{name: "missing value", digest: descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: normalization}},
			{name: "normalisation only", digest: descriptor.Digest{NormalisationAlgorithm: normalization}},
			{name: "mismatching value", digest: descriptor.Digest{HashAlgorithm: "SHA-256", Value: "previous"}, wantError: "digest value mismatch"},
			{name: "mismatching hash", digest: descriptor.Digest{HashAlgorithm: "SHA-512"}, wantError: "hash algorithm mismatch"},
			{name: "unknown normalisation", digest: descriptor.Digest{NormalisationAlgorithm: "other/v1"}, wantError: "normalisation algorithm mismatch"},
		} {
			t.Run(normalization+"/"+tc.name, func(t *testing.T) {
				r := require.New(t)
				scheme := runtime.NewScheme()
				v2.MustAddToScheme(scheme)
				resource := &descriptor.Resource{Digest: tc.digest.DeepCopy()}

				err := updateArtifactAccess(resource, &v2.LocalBlob{}, root, updateAccessOptions{
					Options: Options{AccessScheme: scheme}, NormalisationAlgorithm: normalization,
				})
				if tc.wantError != "" {
					r.ErrorContains(err, tc.wantError)
					r.Equal(&tc.digest, resource.Digest, "a rejected digest must not be rewritten")
					return
				}
				r.NoError(err)
				r.Equal(&descriptor.Digest{
					HashAlgorithm:          "SHA-256",
					NormalisationAlgorithm: normalization,
					Value:                  root.Digest.Encoded(),
				}, resource.Digest)
			})
		}
	}
}

func TestUpdateArtifactAccess_PartialLegacyLabel(t *testing.T) {
	root := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, []byte("test content"))
	for _, tc := range []struct {
		name          string
		set           string
		normalization string
		wantError     string
	}{
		{name: "generic label on OCI artifact is corrected", set: "genericBlobDigest/v1", normalization: "ociArtifactDigest/v1"},
		{name: "OCI label on blob is rejected", set: "ociArtifactDigest/v1", normalization: "genericBlobDigest/v1", wantError: "normalisation algorithm mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			scheme := runtime.NewScheme()
			v2.MustAddToScheme(scheme)
			resource := &descriptor.Resource{Digest: &descriptor.Digest{NormalisationAlgorithm: tc.set, Value: root.Digest.Encoded()}}

			err := updateArtifactAccess(resource, &v2.LocalBlob{}, root, updateAccessOptions{
				Options: Options{AccessScheme: scheme}, NormalisationAlgorithm: tc.normalization,
			})
			if tc.wantError != "" {
				r.ErrorContains(err, tc.wantError)
				return
			}
			r.NoError(err)
			r.Equal(&descriptor.Digest{
				HashAlgorithm:          "SHA-256",
				NormalisationAlgorithm: tc.normalization,
				Value:                  root.Digest.Encoded(),
			}, resource.Digest)
		})
	}
}
