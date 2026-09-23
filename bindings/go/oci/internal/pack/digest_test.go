package pack_test

import (
	"bytes"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociblob "ocm.software/open-component-model/bindings/go/oci/blob"
	"ocm.software/open-component-model/bindings/go/oci/internal/pack"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/oci/tar"
)

func TestPackingLayoutDigestAndContentIntegrity(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()
	payload := []byte("original artifact payload")
	layer := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageLayer, payload)
	var archive bytes.Buffer
	writer, err := tar.NewOCILayoutWriterWithTempFile(&archive, t.TempDir())
	r.NoError(err)
	r.NoError(writer.Push(ctx, layer, bytes.NewReader(payload)))
	root, err := oras.PackManifest(ctx, writer, oras.PackManifestVersion1_1, "application/custom", oras.PackManifestOptions{
		Layers: []ociImageSpecV1.Descriptor{layer},
	})
	r.NoError(err)
	r.NoError(writer.Close())
	r.NotEqual(root.Digest, digest.FromBytes(archive.Bytes()))

	for _, normalization := range []string{"", "ociArtifactDigest/v1", "genericBlobDigest/v1"} {
		for _, corrupt := range []bool{false, true} {
			name := normalization + "/intact"
			if corrupt {
				name = normalization + "/corrupt layer"
			}
			t.Run(name, func(t *testing.T) {
				r := require.New(t)
				data := bytes.Clone(archive.Bytes())
				if corrupt {
					index := bytes.Index(data, payload)
					r.NotEqual(-1, index)
					data[index] ^= 1
				}
				access := &v2.LocalBlob{MediaType: layout.MediaTypeOCIImageLayoutTarV1}
				resource := &descriptor.Resource{Access: access}
				if normalization != "" {
					resource.Digest = &descriptor.Digest{
						HashAlgorithm: "SHA-256", NormalisationAlgorithm: normalization, Value: root.Digest.Encoded(),
					}
				}
				before := resource.Digest.DeepCopy()
				b := &testBlob{content: data, mediaType: layout.MediaTypeOCIImageLayoutTarV1, digest: digest.FromBytes(data)}
				r.NoError(ociblob.UpdateArtifactWithInformationFromBlob(resource, b))
				r.Equal(before, resource.Digest, "archive byte checksums must not become resource digests")
				artifact, err := ociblob.NewArtifactBlob(resource, b)
				r.NoError(err)
				store := memory.New()
				got, err := pack.ArtifactBlob(t.Context(), store, artifact, pack.Options{AccessScheme: v2.Scheme})
				if corrupt {
					r.Error(err, "a valid manifest hash cannot excuse corrupt layer bytes")
					r.Equal(before, resource.Digest)
					return
				}
				r.NoError(err)
				r.Equal(root.Digest, got.Digest)
				if before != nil {
					r.Equal(before, resource.Digest, "legacy normalization must not be rewritten")
				} else {
					r.Equal(&descriptor.Digest{
						HashAlgorithm: "SHA-256", NormalisationAlgorithm: "ociArtifactDigest/v1", Value: root.Digest.Encoded(),
					}, resource.Digest)
				}
				copied, err := content.FetchAll(t.Context(), store, layer)
				r.NoError(err)
				r.Equal(payload, copied)
			})
		}
	}
}

func TestPackingGenericBlobRejectsMismatchedDigest(t *testing.T) {
	r := require.New(t)
	resource := &descriptor.Resource{
		Access: &v2.LocalBlob{MediaType: "application/octet-stream"},
		Digest: &descriptor.Digest{
			HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: digest.FromString("expected bytes").Encoded(),
		},
	}
	// No advertised byte checksum: exercise buffering and packing, rather than
	// just the constructor's known-checksum comparison.
	artifact, err := ociblob.NewArtifactBlob(resource, &testBlob{content: []byte("different bytes"), mediaType: "application/octet-stream"})
	r.NoError(err)
	_, err = pack.ArtifactBlob(t.Context(), memory.New(), artifact, pack.Options{AccessScheme: v2.Scheme})
	r.Error(err)
}
