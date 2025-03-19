package ctf_test

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/ctf/compatibility/artifactset"
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
			r := require.New(t)
			archive, discovered, err := ctf.OpenCTFByFileExtension(tc.path, ctf.O_RDONLY)
			r.Equal(tc.format, discovered, "discovered format should be the same as the one used to open")
			r.NoError(err)
			blobs, err := archive.ListBlobs()
			r.NoError(err)
			r.Len(blobs, 4)
			r.Contains(blobs, "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08")
			idx, err := archive.GetIndex()
			r.NoError(err)
			r.Len(idx.GetArtifacts(), 1)
			artifact := idx.GetArtifacts()[0]
			r.Contains(blobs, artifact.Digest)
			r.Equal("component-descriptors/github.com/acme.org/helloworld", artifact.Repository)
			r.Equal("1.0.0", artifact.Tag)

			r.Error(archive.SetIndex(idx), "should not be able to set index on read-only archive")

			dig := "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
			blob, err := archive.GetBlob(dig)
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
			r := require.New(t)
			err := ctf.WorkWithinCTF(tc.path, ctf.O_RDONLY, func(ctf ctf.CTF) error {
				blobs, err := ctf.ListBlobs()
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
		r := require.New(t)
		archive, err := ctf.OpenCTF("testdata/compatibility/02/without-resource", ctf.FormatDirectory, ctf.O_RDONLY)
		r.NoError(err)
		blobs, err := archive.ListBlobs()
		r.NoError(err)
		r.Len(blobs, 3)
		idx, err := archive.GetIndex()
		r.NoError(err)
		r.Len(idx.GetArtifacts(), 1)
		artifact := idx.GetArtifacts()[0]
		r.Contains(blobs, artifact.Digest)
		r.Equal("component-descriptors/github.com/acme.org/helloworld", artifact.Repository)
		r.Equal("1.0.0", artifact.Tag)

		r.Error(archive.SetIndex(idx), "should not be able to set index on read-only archive")
	})

	t.Run("local (embedded) resource", func(t *testing.T) {
		r := require.New(t)
		archive, err := ctf.OpenCTF("testdata/compatibility/02/with-resource", ctf.FormatDirectory, ctf.O_RDONLY)
		r.NoError(err)
		blobs, err := archive.ListBlobs()
		r.NoError(err)
		r.Len(blobs, 4)
		idx, err := archive.GetIndex()
		r.NoError(err)
		r.Len(idx.GetArtifacts(), 1)
		artifact := idx.GetArtifacts()[0]
		r.Contains(blobs, artifact.Digest)
		r.Equal("component-descriptors/github.com/acme.org/helloworld", artifact.Repository)
		r.Equal("1.0.0", artifact.Tag)

		r.Error(archive.SetIndex(idx), "should not be able to set index on read-only archive")

		// this is the blob containing the local blob.
		// for old CTFs (created with the old OCM reference library) this is a special case
		// as it now contains another (Wrapped) OCI Image layout with a custom format:
		// application/vnd.oci.image.manifest.v1+tar+gzip
		//
		// This format (called "Artifact Set" in old OCM) is a custom format and we dont want to keep this.
		// Instead, we will now access it explicitly as another tgz (wrapped Artifact Set).
		blob, err := archive.GetBlob("sha256:e40e3a2f1ab1a98328dfd14539a79d27aff5c4d5c34cd16a85f0288bfa76490b")
		r.NoError(err)

		as, err := artifactset.NewArtifactSetFromBlob(blob)
		t.Cleanup(func() {
			r.NoError(as.Close())
		})
		r.NoError(err)

		blobs, err = as.ListBlobs()
		r.NoError(err)
		r.Len(blobs, 3)

		artifactSetIndex := as.GetIndex()
		r.Len(artifactSetIndex.Manifests, 1)

		nestedBlob, err := as.GetBlob(artifactSetIndex.Manifests[0].Digest.String())
		r.NoError(err)
		r.IsType(&artifactset.ArtifactBlob{}, nestedBlob)
		nestedBlobStream, err := nestedBlob.ReadCloser()
		r.NoError(err)
		t.Cleanup(func() {
			r.NoError(nestedBlobStream.Close())
		})
		nestedBlobData, err := io.ReadAll(nestedBlobStream)
		r.NoError(err)
		r.NotEmpty(nestedBlobData)
	})
}
