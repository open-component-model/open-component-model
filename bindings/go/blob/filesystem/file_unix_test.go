//go:build unix

package filesystem_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

func TestCopyBlobToOSPath_NamedPipe_Blocking(t *testing.T) {
	r := require.New(t)
	tempDir := t.TempDir()
	pipePath := filepath.Join(tempDir, "pipe")
	r.NoError(unix.Mkfifo(pipePath, 0666))

	testData := []byte("test data")

	fsys, err := filesystem.NewFS(tempDir, os.O_RDWR)
	r.NoError(err)
	filePath := "testfile.txt"
	b := filesystem.NewFileBlob(fsys, filePath)
	writer, err := b.WriteCloser()
	r.NoError(err)
	_, err = writer.Write(testData)
	r.NoError(err)
	r.NoError(writer.Close())

	data := make(chan []byte, 1)
	defer close(data)

	go func() {
		f, err := os.OpenFile(pipePath, os.O_RDONLY, os.ModeNamedPipe)
		r.NoError(err)
		defer func() {
			r.NoError(f.Close())
		}()
		all, err := io.ReadAll(f)
		r.NoError(err)
		data <- all
	}()

	r.NoError(filesystem.CopyBlobToOSPath(b, pipePath))

	r.NoError(err)

	timer := time.NewTimer(1 * time.Second)
	defer timer.Stop()

	select {
	case <-timer.C:
		r.Fail("timeout waiting for data, it never arrived in the pipe")
	case data := <-data:
		r.Equal(testData, data)
	}
}

func Test_GetBlobInWorkingDirectory(t *testing.T) {
	r := require.New(t)
	tempDir := t.TempDir()
	fp := filepath.Join(tempDir, "testfile.txt")
	r.NoError(os.WriteFile(fp, []byte("test data"), 0644))

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
					return
				}
			}

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

func TestEnsurePathInWorkingDirectory(t *testing.T) {
	r := require.New(t)
	tempDir := t.TempDir()
	fp := filepath.Join(tempDir, "testfile.txt")
	r.NoError(os.WriteFile(fp, []byte("test data"), 0644))

	type args struct {
		path             string
		workingDirectory string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "valid path in working directory",
			args: args{
				path:             "testfile.txt",
				workingDirectory: tempDir,
			},
			want:    filepath.Join(tempDir, "testfile.txt"),
			wantErr: false,
		},
		{
			name: "valid absolute path in working directory",
			args: args{
				path:             fp,
				workingDirectory: tempDir,
			},
			want:    fp,
			wantErr: false,
		},
		{
			name: "invalid path escaping working directory",
			args: args{
				path:             "../testfile.txt",
				workingDirectory: tempDir,
			},
			want:    "",
			wantErr: true,
		},
		{
			name: "invalid absolute path not in working directory",
			args: args{
				path:             filepath.Join(tempDir, "../../testfile.txt"),
				workingDirectory: tempDir,
			},
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := filesystem.EnsurePathInWorkingDirectory(tt.args.path, tt.args.workingDirectory)
			if (err != nil) != tt.wantErr {
				t.Errorf("EnsurePathInWorkingDirectory() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("EnsurePathInWorkingDirectory() got = %v, want %v", got, tt.want)
			}
		})
	}
}
