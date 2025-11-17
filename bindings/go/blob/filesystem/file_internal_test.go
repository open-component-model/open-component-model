//go:build unix

package filesystem

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsurePathInWorkingDirectory_Internal(t *testing.T) {
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
			got, err := ensurePathInWorkingDirectory(tt.args.path, tt.args.workingDirectory)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ensurePathInWorkingDirectory() error = %v, wantErr %v", err, tt.wantErr)
				} else {
					t.Logf("ensurePathInWorkingDirectory() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if got != tt.want {
				t.Errorf("ensurePathInWorkingDirectory() got = %v, want %v", got, tt.want)
			}
		})
	}
}
