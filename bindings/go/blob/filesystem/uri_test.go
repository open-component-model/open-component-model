package filesystem_test

import (
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
			name:        "non-file scheme",
			uri:         "https://example.com/file",
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
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}
