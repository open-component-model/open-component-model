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

func TestPackingLayoutVerifiesResolvedRootBeforeWriting(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()
	var archive bytes.Buffer
	writer, err := tar.NewOCILayoutWriterWithTempFile(&archive, t.TempDir())
	r.NoError(err)
	root, err := oras.PackManifest(ctx, writer, oras.PackManifestVersion1_1, "application/selected", oras.PackManifestOptions{})
	r.NoError(err)
	other, err := oras.PackManifest(ctx, writer, oras.PackManifestVersion1_1, "application/other", oras.PackManifestOptions{})
	r.NoError(err)
	r.NotEqual(root.Digest, other.Digest)
	root.Annotations = map[string]string{annotations.OCMLayoutRoot: "true"}
	r.NoError(writer.Tag(ctx, root, root.Digest.String()))
	r.NoError(writer.Close())

	for _, normalization := range []string{"ociArtifactDigest/v1", "genericBlobDigest/v1"} {
		for _, tc := range []struct {
			name      string
			hash      string
			value     string
			wantError string
		}{
			{name: "matching", hash: "SHA-256", value: root.Digest.Encoded()},
			{name: "wrong root", hash: "SHA-256", value: other.Digest.Encoded(), wantError: "digest value mismatch"},
			{name: "wrong hash", hash: "SHA-512", value: root.Digest.Encoded(), wantError: "hash algorithm mismatch"},
			{name: "missing hash", value: root.Digest.Encoded()},
			{name: "missing hash with wrong root", value: other.Digest.Encoded(), wantError: "digest value mismatch"},
			{name: "missing value", hash: "SHA-256"},
			{name: "missing value with wrong hash", hash: "SHA-512", wantError: "hash algorithm mismatch"},
			{name: "normalization only"},
		} {
			t.Run(normalization+"/"+tc.name, func(t *testing.T) {
				r := require.New(t)
				ctx := t.Context()
				access := &v2.LocalBlob{MediaType: layout.MediaTypeOCIImageLayoutTarV1}
				resource := &descriptor.Resource{Access: access, Digest: &descriptor.Digest{
					HashAlgorithm: tc.hash, NormalisationAlgorithm: normalization, Value: tc.value,
				}}
				before := resource.Digest.DeepCopy()
				artifact, err := ociblob.NewArtifactBlob(resource, &testBlob{
					content: archive.Bytes(), mediaType: layout.MediaTypeOCIImageLayoutTarV1, digest: digest.FromBytes(archive.Bytes()),
				})
				r.NoError(err, "the archive checksum is not the resource digest")
				store := &layoutDigestStorage{Storage: memory.New()}
				sentinel := []byte("existing destination content")
				sentinelDesc := content.NewDescriptorFromBytes("application/octet-stream", sentinel)
				r.NoError(store.Storage.Push(ctx, sentinelDesc, bytes.NewReader(sentinel)))

				got, err := pack.ArtifactBlob(ctx, store, artifact, pack.Options{AccessScheme: v2.Scheme})
				if tc.wantError != "" {
					r.ErrorContains(err, tc.wantError)
					r.Zero(store.pushes.Load(), "rejection must precede every destination write")
					r.Equal(before, resource.Digest)
					r.Same(access, resource.Access)
					remaining, err := content.FetchAll(ctx, store, sentinelDesc)
					r.NoError(err)
					r.Equal(sentinel, remaining)
					return
				}
				r.NoError(err)
				r.Equal(root.Digest, got.Digest)
				r.Positive(store.pushes.Load())
				_, err = content.FetchAll(ctx, store, root)
				r.NoError(err)
				if tc.hash != "" && tc.value != "" {
					r.Equal(before, resource.Digest, "preserve the complete signed triple")
				} else {
					r.Equal(&descriptor.Digest{
						HashAlgorithm: "SHA-256", NormalisationAlgorithm: "ociArtifactDigest/v1", Value: root.Digest.Encoded(),
					}, resource.Digest)
				}
			})
		}
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
