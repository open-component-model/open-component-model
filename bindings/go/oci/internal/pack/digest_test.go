package pack_test

import (
	"bytes"
	"context"
	"io"
	"sync/atomic"
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
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/oci/tar"
)

// TestPackingResourceDigest verifies a layout's selected root, never its archive
// checksum, before any destination write, and a generic blob by its bytes.
func TestPackingResourceDigest(t *testing.T) {
	ctx := t.Context()
	payload := []byte("original artifact payload")
	layer := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageLayer, payload)
	var archive bytes.Buffer
	writer, err := tar.NewOCILayoutWriterWithTempFile(&archive, t.TempDir())
	require.NoError(t, err)
	require.NoError(t, writer.Push(ctx, layer, bytes.NewReader(payload)))
	root, err := oras.PackManifest(ctx, writer, oras.PackManifestVersion1_1, "application/selected", oras.PackManifestOptions{
		Layers: []ociImageSpecV1.Descriptor{layer},
	})
	require.NoError(t, err)
	other, err := oras.PackManifest(ctx, writer, oras.PackManifestVersion1_1, "application/other", oras.PackManifestOptions{})
	require.NoError(t, err)
	root.Annotations = map[string]string{annotations.OCMLayoutRoot: "true"}
	require.NoError(t, writer.Tag(ctx, root, root.Digest.String()))
	require.NoError(t, writer.Close())
	require.NotEqual(t, root.Digest, digest.FromBytes(archive.Bytes()))

	type testCase struct {
		name      string
		digest    *descriptor.Digest
		corrupt   bool
		plainBlob bool
		wantError string
	}
	tests := []testCase{
		{name: "nil digest is completed"},
		{name: "nil digest/corrupt layer", corrupt: true, wantError: "mismatched digest"},
		{name: "generic blob with mismatched bytes", plainBlob: true, wantError: "mismatched digest", digest: &descriptor.Digest{
			HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: digest.FromString("expected bytes").Encoded(),
		}},
	}
	for _, normalization := range []string{"ociArtifactDigest/v1", "genericBlobDigest/v1"} {
		for _, tc := range []testCase{
			{name: "matching", digest: &descriptor.Digest{HashAlgorithm: "SHA-256", Value: root.Digest.Encoded()}},
			{name: "matching/corrupt layer", corrupt: true, wantError: "mismatched digest", digest: &descriptor.Digest{HashAlgorithm: "SHA-256", Value: root.Digest.Encoded()}},
			{name: "wrong root", wantError: "digest value mismatch", digest: &descriptor.Digest{HashAlgorithm: "SHA-256", Value: other.Digest.Encoded()}},
			{name: "wrong hash", wantError: "hash algorithm mismatch", digest: &descriptor.Digest{HashAlgorithm: "SHA-512", Value: root.Digest.Encoded()}},
			{name: "missing hash", digest: &descriptor.Digest{Value: root.Digest.Encoded()}},
			{name: "missing hash with wrong root", wantError: "digest value mismatch", digest: &descriptor.Digest{Value: other.Digest.Encoded()}},
			{name: "missing value", digest: &descriptor.Digest{HashAlgorithm: "SHA-256"}},
			{name: "missing value with wrong hash", wantError: "hash algorithm mismatch", digest: &descriptor.Digest{HashAlgorithm: "SHA-512"}},
			{name: "normalization only", digest: &descriptor.Digest{}},
		} {
			tc.name = normalization + "/" + tc.name
			tc.digest.NormalisationAlgorithm = normalization
			tests = append(tests, tc)
		}
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ctx := t.Context()
			data := bytes.Clone(archive.Bytes())
			if tc.corrupt {
				index := bytes.Index(data, payload)
				r.NotEqual(-1, index)
				data[index] ^= 1
			}
			mediaType := layout.MediaTypeOCIImageLayoutTarV1
			b := &testBlob{content: data, mediaType: mediaType, digest: digest.FromBytes(data)}
			if tc.plainBlob {
				// No advertised byte checksum: exercise buffering and packing, rather
				// than just the constructor's known-checksum comparison.
				mediaType = "application/octet-stream"
				b = &testBlob{content: []byte("different bytes"), mediaType: mediaType}
			}
			access := &v2.LocalBlob{MediaType: mediaType}
			resource := &descriptor.Resource{Access: access, Digest: tc.digest.DeepCopy()}
			before := resource.Digest.DeepCopy()
			r.NoError(ociblob.UpdateArtifactWithInformationFromBlob(resource, b))
			r.Equal(before, resource.Digest, "archive byte checksums must not become resource digests")
			artifact, err := ociblob.NewArtifactBlob(resource, b)
			r.NoError(err, "the archive checksum is not the resource digest")
			store := &layoutDigestStorage{Storage: memory.New()}
			sentinel := []byte("existing destination content")
			sentinelDesc := content.NewDescriptorFromBytes("application/octet-stream", sentinel)
			r.NoError(store.Storage.Push(ctx, sentinelDesc, bytes.NewReader(sentinel)))

			got, err := pack.ArtifactBlob(ctx, store, artifact, pack.Options{AccessScheme: v2.Scheme})
			if tc.wantError != "" {
				r.ErrorContains(err, tc.wantError)
				r.Equal(before, resource.Digest)
				if !tc.corrupt && !tc.plainBlob {
					r.Zero(store.pushes.Load(), "root rejection must precede every destination write")
					r.Same(access, resource.Access)
				}
				remaining, err := content.FetchAll(ctx, store, sentinelDesc)
				r.NoError(err)
				r.Equal(sentinel, remaining)
				return
			}
			r.NoError(err)
			r.Equal(root.Digest, got.Digest)
			copied, err := content.FetchAll(ctx, store, layer)
			r.NoError(err)
			r.Equal(payload, copied)
			if before != nil && before.HashAlgorithm != "" && before.Value != "" {
				r.Equal(before, resource.Digest, "preserve the complete signed triple, including a legacy label")
			} else {
				r.Equal(&descriptor.Digest{
					HashAlgorithm: "SHA-256", NormalisationAlgorithm: "ociArtifactDigest/v1", Value: root.Digest.Encoded(),
				}, resource.Digest)
			}
		})
	}
}

type layoutDigestStorage struct {
	content.Storage
	pushes atomic.Int64
}

func (s *layoutDigestStorage) Push(ctx context.Context, desc ociImageSpecV1.Descriptor, reader io.Reader) error {
	s.pushes.Add(1)
	return s.Storage.Push(ctx, desc, reader)
}
