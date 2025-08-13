package dir

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_ensureWdOrPath_Unix(t *testing.T) {
	// check if we are on a Unix-like system
	if os.PathSeparator != '/' {
		t.Skip("Skipping test on non-Unix-like system")
	}

	type args struct {
		path       string
		workingDir string
	}

	tests := []struct {
		name     string
		args     args
		wantPath string
	}{
		{
			name: "absolute path",
			args: args{
				path:       "/absolute/path/to/file.txt",
				workingDir: "",
			},
			wantPath: "/absolute/path/to/file.txt",
		},
		{
			name: "relative path with working dir",
			args: args{
				path:       "relative/path/to/file.txt",
				workingDir: "/current/working/dir",
			},
			wantPath: "/current/working/dir/relative/path/to/file.txt",
		},
		{
			name: "relative path without working dir",
			args: args{
				path:       "relative/path/to/file.txt",
				workingDir: "",
			},
			wantPath: "relative/path/to/file.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ensureWdOrPath(tt.args.path, tt.args.workingDir)
			assert.Equalf(t, tt.wantPath, result, "ensureWdOrPath(%v, %v)", tt.args.path, tt.args.workingDir)
		})
	}
}

func Test_ensureWdOrPath_Windows(t *testing.T) {
	// check if we are on a Windows system
	if os.PathSeparator != '\\' {
		t.Skip("Skipping test on non-Windows system")
	}

	type args struct {
		path       string
		workingDir string
	}

	tests := []struct {
		name     string
		args     args
		wantPath string
	}{
		{
			name: "absolute path",
			args: args{
				path:       "C:\\absolute\\path\\to\\file.txt",
				workingDir: "",
			},
			wantPath: "C:\\absolute\\path\\to\\file.txt",
		},
		{
			name: "relative path with working dir",
			args: args{
				path:       "relative\\path\\to\\file.txt",
				workingDir: "C:\\current\\working\\dir",
			},
			wantPath: "C:\\current\\working\\dir\\relative\\path\\to\\file.txt",
		},
		{
			name: "relative path without working dir",
			args: args{
				path:       "relative\\path\\to\\file.txt",
				workingDir: "",
			},
			wantPath: "relative\\path\\to\\file.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ensureWdOrPath(tt.args.path, tt.args.workingDir)
			assert.Equalf(t, tt.wantPath, result, "ensureWdOrPath(%v, %v)", tt.args.path, tt.args.workingDir)
		})
	}
}
