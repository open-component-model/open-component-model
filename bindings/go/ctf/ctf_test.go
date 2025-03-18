package ctf_test

import (
	"bytes"
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/blob"

	"ocm.software/open-component-model/bindings/go/ctf"
	v1 "ocm.software/open-component-model/bindings/go/ctf/index/v1"
)

func Test_CTF_ReadWrite(t *testing.T) {

	for _, format := range []ctf.FileFormat{
		ctf.FormatDirectory,
		ctf.FormatTAR,
		ctf.FormatTGZ,
	} {
		t.Run(format.String(), func(t *testing.T) {
			r := require.New(t)
			name := "test" + map[ctf.FileFormat]string{
				ctf.FormatDirectory: "",
				ctf.FormatTAR:       ".tar",
				ctf.FormatTGZ:       ".tar.gz",
			}[format]
			path := filepath.Join(t.TempDir(), name)

			testBlob := blob.NewDirectReadOnlyBlob(bytes.NewReader([]byte("test")))
			digest, _ := testBlob.Digest()

			err := ctf.WorkWithinCTF(path, ctf.O_CREATE|ctf.O_RDWR, func(ctf ctf.CTF) error {
				if err := ctf.SaveBlob(testBlob); err != nil {
					return err
				}
				idx, err := ctf.GetIndex()
				if err != nil {
					return err
				}
				idx.AddArtifact(v1.ArtifactMetadata{
					Repository: "test-repo",
					Tag:        "latest",
					Digest:     digest,
				})
				if err := ctf.SetIndex(idx); err != nil {
					return err
				}
				return nil
			})
			r.NoError(err)

			archive, discovered, err := ctf.OpenCTFByFileExtension(path, ctf.O_RDONLY)
			r.NoError(err)
			r.Equal(format, discovered)
			blobs, err := archive.ListBlobs()
			r.NoError(err)
			r.Len(blobs, 1)
			r.Contains(blobs, digest)
			blb, err := archive.GetBlob(digest)
			r.NoError(err)
			r.NotNil(blb)

			data, err := blb.ReadCloser()
			r.NoError(err)
			t.Cleanup(func() {
				r.NoError(data.Close())
			})
			readData, err := io.ReadAll(data)
			r.NoError(err)
			r.Equal("test", string(readData))
		})
	}
}
