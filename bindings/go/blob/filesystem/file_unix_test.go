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
