package file

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "ocm.software/open-component-model/bindings/go/input/file/spec/v1"
)

func Test_ensureAbsolutePath(t *testing.T) {
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
			file := v1.File{Path: tt.args.path}
			err := ensureAbsolutePath(&file, tt.args.workingDir)
			if !tt.wantErr(t, err, fmt.Sprintf("ensureAbsolutePath(%v, %v)", tt.args.path, tt.args.workingDir)) {
				return
			}

			assert.Equalf(t, tt.wantPath, file.Path, "ensureAbsolutePath(%v, %v)", tt.args.path, tt.args.workingDir)
		})
	}
}
