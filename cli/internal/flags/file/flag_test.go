package file

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlag(t *testing.T) {
	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "file-flag-test-*")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, os.RemoveAll(tempDir))
	})

	// Create test files
	regularFile := filepath.Join(tempDir, "regular.txt")
	err = os.WriteFile(regularFile, []byte("test content"), 0644)
	require.NoError(t, err)

	// Test cases
	tests := []struct {
		name            string
		path            string
		wantErr         bool
		errContains     string
		expectDirectory bool
	}{
		{
			name:    "valid regular file",
			path:    regularFile,
			wantErr: false,
		},
		{
			name:    "non-existent file",
			path:    filepath.Join(tempDir, "nonexistent.txt"),
			wantErr: false,
		},
		{
			name:            "directory",
			path:            tempDir,
			wantErr:         false,
			expectDirectory: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flag := &Flag{}
			err := flag.Set(tt.path)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.path, flag.String())
			}
			if tt.expectDirectory {
				assert.Truef(t, flag.IsDir(), "Expected flag to be a directory")
			} else if flag.Exists() {
				assert.Truef(t, flag.Mode().IsRegular(), "Expected flag to be a regular file")
			}
		})
	}
}

func TestFlagVar(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)

	// Test Var
	Var(fs, "test-var", "default.txt", "test usage")
	flag, err := Get(fs, "test-var")
	require.NoError(t, err)
	assert.Equal(t, "default.txt", flag.String())

	// Test VarP
	VarP(fs, "test-var-p", "t", "default-p.txt", "test usage with shorthand")
	flag, err = Get(fs, "test-var-p")
	require.NoError(t, err)
	assert.Equal(t, "default-p.txt", flag.String())
}

func TestFlagOpen(t *testing.T) {
	// Create a temporary file
	tempFile, err := os.CreateTemp("", "file-flag-test-*")
	require.NoError(t, err)
	defer os.Remove(tempFile.Name())

	content := []byte("test content")
	_, err = tempFile.Write(content)
	require.NoError(t, err)
	tempFile.Close()

	// Test opening existing file
	flag := &Flag{}
	err = flag.Set(tempFile.Name())
	require.NoError(t, err)

	reader, err := flag.Open()
	require.NoError(t, err)
	defer reader.Close()

	readContent, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, content, readContent)

	// Test opening non-existent file
	flag = &Flag{}
	err = flag.Set("nonexistent.txt")
	require.NoError(t, err)

	_, err = flag.Open()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestFlagType(t *testing.T) {
	flag := &Flag{}
	assert.Equal(t, Type, flag.Type())
}
