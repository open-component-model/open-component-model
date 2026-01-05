package ctf_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/nlepage/go-tarfs"
	"github.com/opencontainers/go-digest"
	ociimagespecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/ctf"
)

// Test_CTF_Basic_ReadOnly_Compatibility tests the compatibility of CTF archives
// created with the old OCM reference library for read-only scenarios. (our only supported case for old CTFs)
func Test_CTF_Basic_ReadOnly_Compatibility(t *testing.T) {
	for _, tc := range []struct {
		name   string
		path   string
		format ctf.FileFormat
	}{
		{
			name:   "Directory",
			path:   "testdata/compatibility/01/transport-archive",
			format: ctf.FormatDirectory,
		},
		{
			name:   "Tar",
			path:   "testdata/compatibility/01/transport-archive.tar",
			format: ctf.FormatTAR,
		},
		{
			name:   "TarGz",
			path:   "testdata/compatibility/01/transport-archive.tar.gz",
			format: ctf.FormatTGZ,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			r := require.New(t)
			archive, discovered, err := ctf.OpenCTFByFileExtension(ctx, ctf.OpenCTFOptions{
				Path:    tc.path,
				Flag:    ctf.O_RDONLY,
				TempDir: t.TempDir(),
			})
			r.Equal(tc.format, discovered, "discovered format should be the same as the one used to open")
			r.NoError(err)
			blobs, err := archive.ListBlobs(ctx)
			r.NoError(err)
			r.Len(blobs, 4)
			r.Contains(blobs, "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08")
			idx, err := archive.GetIndex(ctx)
			r.NoError(err)
			r.Len(idx.GetArtifacts(), 1)
			artifact := idx.GetArtifacts()[0]
			r.Contains(blobs, artifact.Digest)
			r.Equal("component-descriptors/github.com/acme.org/helloworld", artifact.Repository)
			r.Equal("1.0.0", artifact.Tag)

			r.Error(archive.SetIndex(ctx, idx), "should not be able to set index on read-only archive")

			dig := "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
			blob, err := archive.GetBlob(ctx, dig)
			r.NoError(err)
			r.NotNil(blob)
			r.IsType(&ctf.CASFileBlob{}, blob)
			r.True(blob.(*ctf.CASFileBlob).HasPrecalculatedDigest())
			digFromBlob, known := blob.(*ctf.CASFileBlob).Digest()
			r.True(known)
			r.Equal(dig, digFromBlob)

			readCloser, err := blob.ReadCloser()
			r.NoError(err)
			t.Cleanup(func() {
				r.NoError(readCloser.Close())
			})
			data, err := io.ReadAll(readCloser)
			r.NoError(err)
			r.Equal("test", string(data))
		})

		t.Run("work within "+tc.name, func(t *testing.T) {
			ctx := t.Context()
			r := require.New(t)
			err := ctf.WorkWithinCTF(ctx, ctf.OpenCTFOptions{
				Path:    tc.path,
				Flag:    ctf.O_RDONLY,
				TempDir: t.TempDir(),
			}, func(ctx context.Context, ctf ctf.CTF) error {
				blobs, err := ctf.ListBlobs(ctx)
				if err != nil {
					return err
				}
				r.Len(blobs, 4)
				return nil
			})
			r.NoError(err, "should be able to work within CTF")
		})
	}
}

// Test_CTF_Advanced_ReadOnly_Compatibility tests the compatibility of CTF archives
// that have advanced properties such as remote or local accesses in their descriptors.
func Test_CTF_Advanced_ReadOnly_Compatibility(t *testing.T) {
	t.Run("remote resource", func(t *testing.T) {
		ctx := t.Context()
		r := require.New(t)
		archive, err := ctf.OpenCTF(ctx, ctf.OpenCTFOptions{
			Path:   "testdata/compatibility/02/without-resource",
			Format: ctf.FormatDirectory,
			Flag:   ctf.O_RDONLY,
		})
		r.NoError(err)
		blobs, err := archive.ListBlobs(ctx)
		r.NoError(err)
		r.Len(blobs, 3)
		idx, err := archive.GetIndex(ctx)
		r.NoError(err)
		r.Len(idx.GetArtifacts(), 1)
		artifact := idx.GetArtifacts()[0]
		r.Contains(blobs, artifact.Digest)
		r.Equal("component-descriptors/github.com/acme.org/helloworld", artifact.Repository)
		r.Equal("1.0.0", artifact.Tag)

		r.Error(archive.SetIndex(ctx, idx), "should not be able to set index on read-only archive")
	})

	t.Run("local (embedded) resource", func(t *testing.T) {
		ctx := t.Context()
		r := require.New(t)
		archive, err := ctf.OpenCTF(ctx, ctf.OpenCTFOptions{
			Path:   "testdata/compatibility/02/with-resource",
			Format: ctf.FormatDirectory,
			Flag:   ctf.O_RDONLY,
		})
		r.NoError(err)
		blobs, err := archive.ListBlobs(ctx)
		r.NoError(err)
		r.Len(blobs, 4)
		idx, err := archive.GetIndex(ctx)
		r.NoError(err)
		r.Len(idx.GetArtifacts(), 1)
		artifact := idx.GetArtifacts()[0]
		r.Contains(blobs, artifact.Digest)
		r.Equal("component-descriptors/github.com/acme.org/helloworld", artifact.Repository)
		r.Equal("1.0.0", artifact.Tag)

		r.Error(archive.SetIndex(ctx, idx), "should not be able to set index on read-only archive")

		// this is the blob containing the local blob.
		// for old CTFs (created with the old OCM reference library) this is a special case
		// as it now contains another (Wrapped) OCI Image layout with a custom format:
		// application/vnd.oci.image.manifest.v1+tar+gzip
		//
		// This format (called "Artifact Set" in old OCM) is a custom format and we dont want to keep this.
		// Instead, we will now access it explicitly as another tgz (wrapped Artifact Set).
		blob, err := archive.GetBlob(ctx, "sha256:e40e3a2f1ab1a98328dfd14539a79d27aff5c4d5c34cd16a85f0288bfa76490b")
		r.NoError(err)

		t.Run("interpret as artifact set", func(t *testing.T) {
			r := require.New(t)
			as, err := ctf.NewArtifactSetFromBlob(blob)
			t.Cleanup(func() {
				r.NoError(as.Close())
			})
			r.NoError(err)

			r.Equal(as.Format(), ctf.ArtifactSetFormatLegacyOCI)

			blobs, err = as.ListBlobs(ctx)
			r.NoError(err)
			r.Len(blobs, 3)

			artifactSetIndex := as.GetIndex()
			r.Len(artifactSetIndex.Manifests, 1)

			nestedBlob, err := as.GetBlob(ctx, artifactSetIndex.Manifests[0].Digest.String())
			r.NoError(err)
			r.IsType(&ctf.ArtifactBlob{}, nestedBlob)
			nestedBlobStream, err := nestedBlob.ReadCloser()
			r.NoError(err)
			t.Cleanup(func() {
				r.NoError(nestedBlobStream.Close())
			})
			nestedBlobData, err := io.ReadAll(nestedBlobStream)
			r.NoError(err)
			r.NotEmpty(nestedBlobData)

			t.Run("convert to OCI image layout", func(t *testing.T) {
				r := require.New(t)
				prefixFromDescriptor := "my-repo-from-external-descriptor/my-image"
				var buf bytes.Buffer
				r.NoError(ctf.ConvertToOCIImageLayout(ctx, as, &buf, func(_ context.Context, digest digest.Digest, oldName string) (string, error) {
					return fmt.Sprintf("%s:%s@%s", prefixFromDescriptor, oldName, digest.String()), nil
				}))

				ociLayoutFs, err := tarfs.New(&buf)
				r.NoError(err)

				rawOCIIndex, err := ociLayoutFs.Open("index.json")
				r.NoError(err)
				t.Cleanup(func() {
					r.NoError(rawOCIIndex.Close())
				})

				index := ociimagespecv1.Index{}
				r.NoError(json.NewDecoder(rawOCIIndex).Decode(&index))
				r.Len(index.Manifests, 1)
				r.NotNil(artifactSetIndex.Manifests[0].Annotations[ociimagespecv1.AnnotationRefName])
				r.Equal(
					fmt.Sprintf("%s:%s@%s", prefixFromDescriptor, "6.7.1", "sha256:62be4af3382a4493cb7f1dd4ec47bcb28f1863b615fc9e4a1dceefbe93898dd0"),
					artifactSetIndex.Manifests[0].Annotations[ociimagespecv1.AnnotationRefName],
				)

				dirfs := ociLayoutFs.(fs.ReadDirFS)
				entries, err := dirfs.ReadDir(filepath.Join("blobs", "sha256"))
				r.NoError(err)
				r.Len(entries, 3)
			})
		})
	})
}
