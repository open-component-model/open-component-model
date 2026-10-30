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

	"ocm.software/open-component-model/bindings/go/blob"
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

// A symlink at the target path is kept and the content is written to its target.
func TestCopyBlobToOSPathWritesThroughSymlink(t *testing.T) {
	r := require.New(t)
	tempDir := t.TempDir()
	target := filepath.Join(tempDir, "target.txt")
	r.NoError(os.WriteFile(target, []byte("old"), 0o600))
	link := filepath.Join(tempDir, "link.txt")
	r.NoError(os.Symlink(target, link))

	r.NoError(filesystem.CopyBlobToOSPath(readerBlob{strings.NewReader("new")}, link))

	fi, err := os.Lstat(link)
	r.NoError(err)
	r.NotZero(fi.Mode()&os.ModeSymlink, "the link must not be replaced")
	got, err := os.ReadFile(target)
	r.NoError(err)
	r.Equal("new", string(got))
}

// After a failed copy through a symlink the link is kept and its target holds no rejected content.
func TestCopyBlobToOSPathEmptiesSymlinkTargetWhenCloseFails(t *testing.T) {
	r := require.New(t)
	tempDir := t.TempDir()
	target := filepath.Join(tempDir, "target.txt")
	r.NoError(os.WriteFile(target, []byte("old"), 0o600))
	link := filepath.Join(tempDir, "link.txt")
	r.NoError(os.Symlink(target, link))

	err := filesystem.CopyBlobToOSPath(closeErrBlob{strings.NewReader("new")}, link)
	r.ErrorContains(err, "verification failed on close")

	fi, err := os.Lstat(link)
	r.NoError(err)
	r.NotZero(fi.Mode()&os.ModeSymlink, "the link must not be removed")
	got, err := os.ReadFile(target)
	r.NoError(err)
	r.Empty(got)
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

func Test_GetBlobInWorkingDirectory(t *testing.T) {
	r := require.New(t)
	tempDir := t.TempDir()
	fp := filepath.Join(tempDir, "testfile.txt")
	r.NoError(os.WriteFile(fp, []byte("test data"), 0o644))

	type args struct {
		path       string
		workingDir string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "valid relative path in working directory",
			args: args{
				path:       "testfile.txt",
				workingDir: tempDir,
			},
			wantErr: false,
		},
		{
			name: "valid absolute path in working directory",
			args: args{
				path:       fp,
				workingDir: tempDir,
			},
			wantErr: false,
		},
		{
			name: "invalid path escaping working directory",
			args: args{
				path:       "../testfile.txt",
				workingDir: tempDir,
			},
			wantErr: true,
		},
		{
			name: "invalid absolute path not in working directory",
			args: args{
				path:       filepath.Join(tempDir, "../../testfile.txt"),
				workingDir: tempDir,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := filesystem.GetBlobInWorkingDirectory(tt.args.path, tt.args.workingDir)
			if tt.wantErr {
				if err == nil {
					t.Errorf("GetBlobInWorkingDirectory() expected error, got nil")
				} else {
					t.Logf("GetBlobInWorkingDirectory() expected error, got: %v", err)
				}
				return
			}

			r.NoError(err, "GetBlobInWorkingDirectory() unexpected error")

			reader, err := got.ReadCloser()
			r.NoError(err)
			defer func(reader io.ReadCloser) {
				_ = reader.Close()
			}(reader)

			data, err := io.ReadAll(reader)
			r.NoError(err)
			r.Equal("test data", string(data))
		})
	}
}

// A symlink to a file in another directory is followed, like os.Open.
func TestGetBlobFromOSPathFollowsSymlinkOutOfParent(t *testing.T) {
	r := require.New(t)
	tempDir := t.TempDir()
	r.NoError(os.Mkdir(filepath.Join(tempDir, "a"), 0o700))
	r.NoError(os.Mkdir(filepath.Join(tempDir, "b"), 0o700))
	r.NoError(os.WriteFile(filepath.Join(tempDir, "b", "f"), []byte("data"), 0o600))
	link := filepath.Join(tempDir, "a", "link")
	r.NoError(os.Symlink(filepath.Join("..", "b", "f"), link))

	b, err := filesystem.GetBlobFromOSPath(link)
	r.NoError(err)
	r.Equal(int64(len("data")), b.Size())
	reader, err := b.ReadCloser()
	r.NoError(err)
	t.Cleanup(func() { _ = reader.Close() })
	data, err := io.ReadAll(reader)
	r.NoError(err)
	r.Equal("data", string(data))
}

// Symlinks are confined to the working directory, not to the file's parent directory,
// and confinement holds on every read, not only when the blob is created.
func TestGetBlobInWorkingDirectorySymlinks(t *testing.T) {
	setup := func(t *testing.T) (workDir, outside string) {
		t.Helper()
		r := require.New(t)
		workDir = t.TempDir()
		r.NoError(os.Mkdir(filepath.Join(workDir, "a"), 0o700))
		r.NoError(os.Mkdir(filepath.Join(workDir, "b"), 0o700))
		r.NoError(os.WriteFile(filepath.Join(workDir, "b", "f"), []byte("inside"), 0o600))
		outside = filepath.Join(t.TempDir(), "secret")
		r.NoError(os.WriteFile(outside, []byte("outside"), 0o600))
		return workDir, outside
	}

	t.Run("link leaving parent but staying in working directory is read", func(t *testing.T) {
		r := require.New(t)
		workDir, _ := setup(t)
		r.NoError(os.Symlink(filepath.Join("..", "b", "f"), filepath.Join(workDir, "a", "link")))

		b, err := filesystem.GetBlobInWorkingDirectory(filepath.Join("a", "link"), workDir)
		r.NoError(err)
		r.Equal(int64(len("inside")), b.Size())
		reader, err := b.ReadCloser()
		r.NoError(err)
		t.Cleanup(func() { _ = reader.Close() })
		data, err := io.ReadAll(reader)
		r.NoError(err)
		r.Equal("inside", string(data))
	})

	t.Run("link escaping working directory is rejected on creation", func(t *testing.T) {
		r := require.New(t)
		workDir, outside := setup(t)
		r.NoError(os.Symlink(outside, filepath.Join(workDir, "a", "link")))

		_, err := filesystem.GetBlobInWorkingDirectory(filepath.Join("a", "link"), workDir)
		r.Error(err)
	})

	t.Run("file swapped for escaping link after creation is rejected on read", func(t *testing.T) {
		r := require.New(t)
		workDir, outside := setup(t)
		file := filepath.Join(workDir, "b", "f")

		b, err := filesystem.GetBlobInWorkingDirectory(file, workDir)
		r.NoError(err)
		r.NoError(os.Remove(file))
		r.NoError(os.Symlink(outside, file))

		_, err = b.ReadCloser()
		r.ErrorContains(err, "path escapes from parent")
		r.Equal(blob.SizeUnknown, b.Size())
	})
}
