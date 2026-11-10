package filesystem_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

func TestFilePathFromURI(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		expected    string
		expectError bool
	}{
		{
			name:     "standard file URI",
			uri:      "file:///tmp/some-file",
			expected: "/tmp/some-file",
		},
		{
			name:     "file URI with nested path",
			uri:      "file:///var/tmp/buffers/resource-001",
			expected: "/var/tmp/buffers/resource-001",
		},
		{
			name:     "localhost host accepted",
			uri:      "file://localhost/tmp/file",
			expected: "/tmp/file",
		},
		{
			name:     "LOCALHOST accepted case-insensitively",
			uri:      "file://LOCALHOST/tmp/file",
			expected: "/tmp/file",
		},
		{
			name:        "non-file scheme",
			uri:         "https://example.com/file",
			expectError: true,
		},
		{
			// RFC 8089 requires the scheme; the File spec documents the URI as
			// conforming to it, so a bare path is not a shorthand for one.
			name:        "plain path without scheme",
			uri:         "/tmp/some-file",
			expectError: true,
		},
		{
			name:        "invalid URI",
			uri:         "://broken",
			expectError: true,
		},
		{
			name:        "opaque file URI",
			uri:         "file:relative/path",
			expectError: true,
		},
		{
			name:        "remote host rejected",
			uri:         "file://remotehost/path/to/file",
			expectError: true,
		},
		{
			name:        "empty path",
			uri:         "file://",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := filesystem.FilePathFromURI(tt.uri)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, filepath.FromSlash(tt.expected), result)
			}
		})
	}
}

func TestFilePathFromURI_WindowsDrive(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drive letters only form a volume on windows")
	}
	r := require.New(t)
	result, err := filesystem.FilePathFromURI("file:///C:/Users/some%20dir/file")
	r.NoError(err)
	r.Equal(`C:\Users\some dir\file`, result)
}

func TestURIFromFilePath_Roundtrip(t *testing.T) {
	r := require.New(t)
	path := filepath.Join(t.TempDir(), "dir with space", "file#1.txt")

	uri, err := filesystem.URIFromFilePath(path)
	r.NoError(err)
	r.Regexp(`^file:///[^/]`, uri)

	result, err := filesystem.FilePathFromURI(uri)
	r.NoError(err)
	r.Equal(path, result)
}

func TestURIFromFilePath_RelativePathRejected(t *testing.T) {
	r := require.New(t)

	_, err := filesystem.URIFromFilePath(filepath.Join("rel", "file.txt"))
	r.ErrorContains(err, "absolute path")
}
