package filesystem_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

func TestCopyBlobToOSPath(t *testing.T) {
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

	copyPath := filepath.Join(tempDir, "copy.txt")
	r.NoError(filesystem.CopyBlobToOSPath(b, copyPath))

	copiedData, err := os.ReadFile(copyPath)
	r.NoError(err)
	r.Equal("test data", string(copiedData))
}

func TestGetLocalBlobFromLocation(t *testing.T) {
	r := require.New(t)
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "testfile.txt")
	r.NoError(os.WriteFile(filePath, []byte("test data"), 0644))

	b, err := filesystem.GetBlobFromOSPath(filePath)
	r.NoError(err)

	reader, err := b.ReadCloser()
	r.NoError(err)
	defer reader.Close()

	data, err := io.ReadAll(reader)
	r.NoError(err)
	r.Equal("test data", string(data))
}
