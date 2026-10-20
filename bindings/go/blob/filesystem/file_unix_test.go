//go:build unix

package filesystem_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

type readerBlob struct{ io.Reader }

func (b readerBlob) ReadCloser() (io.ReadCloser, error) { return io.NopCloser(b.Reader), nil }

func TestCopyBlobToOSPathWritesIntoNamedPipe(t *testing.T) {
	r := require.New(t)
	pipe := filepath.Join(t.TempDir(), "pipe")
	r.NoError(syscall.Mkfifo(pipe, 0o600))

	read := make(chan string, 1)
	go func() {
		f, err := os.Open(pipe)
		if err != nil {
			read <- "open: " + err.Error()
			return
		}
		got, err := io.ReadAll(f)
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			read <- "read: " + err.Error()
			return
		}
		read <- string(got)
	}()

	r.NoError(filesystem.CopyBlobToOSPath(readerBlob{strings.NewReader("new")}, pipe))
	r.Equal("new", <-read)

	fi, err := os.Lstat(pipe)
	r.NoError(err)
	r.NotZero(fi.Mode()&os.ModeNamedPipe, "the pipe must not be replaced by a regular file")
}

// A symlink at the target path is replaced by a regular file; its former target keeps its content.
func TestCopyBlobToOSPathReplacesSymlink(t *testing.T) {
	r := require.New(t)
	tempDir := t.TempDir()
	target := filepath.Join(tempDir, "target.txt")
	r.NoError(os.WriteFile(target, []byte("old"), 0o600))
	link := filepath.Join(tempDir, "link.txt")
	r.NoError(os.Symlink(target, link))

	r.NoError(filesystem.CopyBlobToOSPath(readerBlob{strings.NewReader("new")}, link))

	fi, err := os.Lstat(link)
	r.NoError(err)
	r.True(fi.Mode().IsRegular(), "the link must be replaced by a regular file")
	got, err := os.ReadFile(link)
	r.NoError(err)
	r.Equal("new", string(got))
	got, err = os.ReadFile(target)
	r.NoError(err)
	r.Equal("old", string(got))
}
