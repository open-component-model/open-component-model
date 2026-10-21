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

	// Checked before receiving: a replaced pipe never gets a writer, so the receive would block.
	fi, err := os.Lstat(pipe)
	r.NoError(err)
	r.NotZero(fi.Mode()&os.ModeNamedPipe, "the pipe must not be replaced by a regular file")
	r.Equal("new", <-read)
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

func TestCopyBlobToOSPathKeepsPermissionsOfExistingFile(t *testing.T) {
	r := require.New(t)
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "src.txt")
	r.NoError(os.WriteFile(src, []byte("new"), 0o600))
	b, err := filesystem.GetBlobFromOSPath(src)
	r.NoError(err)

	dst := filepath.Join(tempDir, "dst.txt")
	r.NoError(os.WriteFile(dst, []byte("old"), 0o600))
	r.NoError(os.Chmod(dst, 0o644))
	r.NoError(filesystem.CopyBlobToOSPath(b, dst))

	fi, err := os.Stat(dst)
	r.NoError(err)
	r.Equal(os.FileMode(0o644), fi.Mode().Perm())
}
