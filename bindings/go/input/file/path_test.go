package file_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/input/file"
)

func Test_IsAbsolutePath(t *testing.T) {
	type args struct {
		path string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "absolute path on Unix",
			args: args{path: "/absolute/path/to/file.txt"},
			want: true,
		},
		{
			name: "absolute path on Windows",
			args: args{path: "C:\\absolute\\path\\to\\file.txt"},
			want: true,
		},
		{
			name: "relative path",
			args: args{path: "relative/path/to/file.txt"},
			want: false,
		},
		{
			name: "empty path",
			args: args{path: ""},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, file.IsAbsolutePath(tt.args.path), "isAbsolutePath(%v)", tt.args.path)
		})
	}
}

func Test_EnsureAbsolutePath(t *testing.T) {
	type args struct {
		path       string
		workingDir string
	}

	wd, err := os.Getwd()
	require.NoError(t, err, "Failed to get current working directory")

	tests := []struct {
		name     string
		args     args
		wantPath string
		wantErr  assert.ErrorAssertionFunc
	}{
		{
			name: "absolute path",
			args: args{
				path:       "/absolute/path/to/file.txt",
				workingDir: "",
			},
			wantPath: "/absolute/path/to/file.txt",
			wantErr:  assert.NoError,
		},
		{
			name: "relative path with working dir",
			args: args{
				path:       "relative/path/to/file.txt",
				workingDir: "/current/working/dir",
			},
			wantPath: "/current/working/dir/relative/path/to/file.txt",
			wantErr:  assert.NoError,
		},
		{
			name: "relative path without working dir",
			args: args{
				path:       "relative/path/to/file.txt",
				workingDir: "",
			},
			wantPath: fmt.Sprintf("%s/relative/path/to/file.txt", wd),
			wantErr:  assert.NoError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.args.path
			err := file.EnsureAbsolutePath(&path, tt.args.workingDir)
			if !tt.wantErr(t, err, fmt.Sprintf("ensureAbsolutePath(%v, %v)", tt.args.path, tt.args.workingDir)) {
				return
			}

			assert.Equalf(t, tt.wantPath, path, "ensureAbsolutePath(%v, %v)", tt.args.path, tt.args.workingDir)
		})
	}
}
