package filesystem_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

type readerBlob struct{ io.Reader }

func (b readerBlob) ReadCloser() (io.ReadCloser, error) { return io.NopCloser(b.Reader), nil }

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
	r.NoError(os.WriteFile(filePath, []byte("test data"), 0o644))

	b, err := filesystem.GetBlobFromOSPath(filePath)
	r.NoError(err)

	reader, err := b.ReadCloser()
	r.NoError(err)
	defer func() {
		r.NoError(reader.Close())
	}()

	data, err := io.ReadAll(reader)
	r.NoError(err)
	r.Equal("test data", string(data))
}

func TestCopyBlobToOSPathReplacesExistingFile(t *testing.T) {
	r := require.New(t)
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "src.txt")
	r.NoError(os.WriteFile(src, []byte("new"), 0o600))
	b, err := filesystem.GetBlobFromOSPath(src)
	r.NoError(err)

	dst := filepath.Join(tempDir, "dst.txt")
	r.NoError(os.WriteFile(dst, []byte("old"), 0o600))
	r.NoError(filesystem.CopyBlobToOSPath(b, dst))

	got, err := os.ReadFile(dst)
	r.NoError(err)
	r.Equal("new", string(got))
}

type closeErrBlob struct{ io.Reader }

func (b closeErrBlob) ReadCloser() (io.ReadCloser, error) { return b, nil }
func (closeErrBlob) Close() error                         { return errors.New("verification failed on close") }

func TestCopyBlobToOSPathRemovesTargetWhenCloseFails(t *testing.T) {
	r := require.New(t)
	dst := filepath.Join(t.TempDir(), "dst.txt")
	r.NoError(os.WriteFile(dst, []byte("old"), 0o600))

	err := filesystem.CopyBlobToOSPath(closeErrBlob{strings.NewReader("new")}, dst)
	r.ErrorContains(err, "verification failed on close")
	r.NoFileExists(dst, "content that failed verification must not be left at the target path")
}

func TestCopyBlobToOSPathAcceptsLongFileName(t *testing.T) {
	r := require.New(t)
	dst := filepath.Join(t.TempDir(), strings.Repeat("a", 250))
	r.NoError(filesystem.CopyBlobToOSPath(readerBlob{strings.NewReader("new")}, dst))

	got, err := os.ReadFile(dst)
	r.NoError(err)
	r.Equal("new", string(got))
}
