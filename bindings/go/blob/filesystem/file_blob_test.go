package filesystem_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

func TestBlob_ReadCloser(t *testing.T) {
	r := require.New(t)
	tempDir := t.TempDir()
	fsys, err := filesystem.NewFS(tempDir, os.O_RDWR)
	r.NoError(err)

	filePath := "testfile.txt"
	_, err = fsys.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0644)
	r.NoError(err)

	b := filesystem.NewFileBlob(fsys, filePath)
	reader, err := b.ReadCloser()
	r.NoError(err)
	r.NoError(reader.Close())
}

func TestBlob_WriteCloser(t *testing.T) {
	r := require.New(t)
	tempDir := t.TempDir()
	fsys, err := filesystem.NewFS(tempDir, os.O_RDWR)
	r.NoError(err)

	filePath := "testfile.txt"
	b := filesystem.NewFileBlob(fsys, filePath)

	writer, err := b.WriteCloser()
	r.NoError(err)

	_, err = writer.Write([]byte("test data"))
	r.NoError(err)
	r.NoError(writer.Close())
}

func TestBlob_Size(t *testing.T) {
	r := require.New(t)
	tempDir := t.TempDir()
	fsys, err := filesystem.NewFS(tempDir, os.O_RDWR)
	r.NoError(err)

	filePath := "testfile.txt"
	b := filesystem.NewFileBlob(fsys, filePath)

	writer, err := b.WriteCloser()
	r.NoError(err)
	_, err = writer.Write([]byte("test data"))
	r.NoError(err)
	r.NoError(writer.Close())

	size := b.Size()
	r.Greater(size, int64(blob.SizeUnknown))
	r.Equal(int64(9), size)
}

func TestBlob_Digest(t *testing.T) {
	r := require.New(t)
	tempDir := t.TempDir()
	fsys, err := filesystem.NewFS(tempDir, os.O_RDWR)
	r.NoError(err)

	filePath := "testfile.txt"
	b := filesystem.NewFileBlob(fsys, filePath)

	writer, err := b.WriteCloser()
	r.NoError(err)
	_, err = writer.Write([]byte("test data"))
	r.NoError(err)
	r.NoError(writer.Close())

	digestStr, ok := b.Digest()
	r.True(ok)

	data, err := b.ReadCloser()
	r.NoError(err)
	defer data.Close()

	var buf bytes.Buffer
	expectedDigest, err := digest.FromReader(io.TeeReader(data, &buf))
	r.NoError(err)
	r.Equal(expectedDigest.String(), digestStr)
}

func TestBlob_FS_Compat(t *testing.T) {
	r := require.New(t)
	td := []byte("test data")
	fs := fstest.MapFS{
		"testfile.txt": &fstest.MapFile{
			Data:    td,
			Mode:    0644,
			ModTime: time.Now(),
		},
	}

	b := filesystem.NewFileBlob(fs, "testfile.txt")
	reader, err := b.ReadCloser()
	r.NoError(err)
	data, err := io.ReadAll(reader)
	r.NoError(err)
	r.Equal(td, data)
	r.NoError(reader.Close())

	_, err = b.WriteCloser()
	r.Error(err)
	r.Equal(b.Size(), int64(len(td)))
}

func TestBlob_MediaType(t *testing.T) {
	r := require.New(t)
	tempDir := t.TempDir()
	fsys, err := filesystem.NewFS(tempDir, os.O_RDWR)
	r.NoError(err)

	filePath := "testfile.txt"
	b := filesystem.NewFileBlob(fsys, filePath)

	// Initially no media type
	mediaType, ok := b.MediaType()
	r.False(ok)
	r.Equal("", mediaType)

	// Set media type
	expectedMediaType := "text/plain"
	b.SetMediaType(expectedMediaType)

	// Verify media type is set
	mediaType, ok = b.MediaType()
	r.True(ok)
	r.Equal(expectedMediaType, mediaType)

	// Override with different media type
	newMediaType := "application/json"
	b.SetMediaType(newMediaType)
	mediaType, ok = b.MediaType()
	r.True(ok)
	r.Equal(newMediaType, mediaType)
}

func TestBlob_FromPath(t *testing.T) {
	t.Run("base is read only", func(t *testing.T) {
		td := []byte("bar")
		r := require.New(t)
		filePath := filepath.Join(t.TempDir(), "foo")
		r.NoError(os.WriteFile(filePath, td, 0644))

		b, err := filesystem.GetBlobFromOSPath(filePath)
		r.NoError(err)
		reader, err := b.ReadCloser()
		r.NoError(err)
		data, err := io.ReadAll(reader)
		r.NoError(err)
		r.Equal(td, data)
		r.NoError(reader.Close())

		_, err = b.WriteCloser()
		r.Error(err)
		r.Equal(b.Size(), int64(len(td)))
	})

	t.Run("if flag is rdwr, it is read/write", func(t *testing.T) {
		td := []byte("bar")
		r := require.New(t)
		filePath := filepath.Join(t.TempDir(), "foo")
		r.NoError(os.WriteFile(filePath, td, 0644))

		b, err := filesystem.NewFileBlobFromPathWithFlag(filePath, os.O_RDWR)
		r.NoError(err)
		reader, err := b.ReadCloser()
		r.NoError(err)
		data, err := io.ReadAll(reader)
		r.NoError(err)
		r.Equal(td, data)
		r.NoError(reader.Close())

		writer, err := b.WriteCloser()
		r.NoError(err)
		_, err = writer.Write([]byte("test data"))
		r.NoError(err)
		r.NoError(writer.Close())
	})
}
