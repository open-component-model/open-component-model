package transformer_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/oci/transformer"
)

func Test_DetermineOutputPath_EmptyPath(t *testing.T) {
	prefix := "test-prefix"
	outputPath, err := transformer.DetermineOutputPath("", prefix)

	require.NoError(t, err)
	assert.NotEmpty(t, outputPath)
	assert.Contains(t, filepath.Base(outputPath), prefix)

	// Verify file exists and clean up
	_, err = os.Stat(outputPath)
	assert.NoError(t, err)
	_ = os.Remove(outputPath)
}

func Test_DetermineOutputPath_DirectoryPath(t *testing.T) {
	tempDir := t.TempDir()
	prefix := "test-prefix"

	outputPath, err := transformer.DetermineOutputPath(tempDir, prefix)

	require.NoError(t, err)
	assert.Contains(t, filepath.Base(outputPath), prefix)
	assert.True(t, strings.HasPrefix(outputPath, tempDir))
	// ensure the outputPath is a file, not a directory
	info, err := os.Stat(outputPath)
	require.NoError(t, err)
	assert.False(t, info.IsDir())

	// Verify file exists and clean up
	_, err = os.Stat(outputPath)
	assert.NoError(t, err)
}

func Test_DetermineOutputPath_NonExistentFilePath(t *testing.T) {
	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "nonexistent.tar.gz")

	result, err := transformer.DetermineOutputPath(outputPath, "test-prefix")

	require.NoError(t, err)
	assert.Equal(t, outputPath, result)
	// filePrefix must be ignored when a full file path is provided
	assert.NotContains(t, filepath.Base(result), "test-prefix")
}

func Test_DetermineOutputPath_ProvidedPath(t *testing.T) {
	tests := []struct {
		name     string
		setupDir func(*testing.T) (string, string) // returns tempDir, outputPath
	}{
		{
			name: "existing directory",
			setupDir: func(t *testing.T) (string, string) {
				tempDir := t.TempDir()
				return tempDir, filepath.Join(tempDir, "output.tar.gz")
			},
		},
		{
			name: "nested directories",
			setupDir: func(t *testing.T) (string, string) {
				tempDir := t.TempDir()
				return tempDir, filepath.Join(tempDir, "level1", "level2", "output.tar.gz")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir, outputPath := tt.setupDir(t)

			result, err := transformer.DetermineOutputPath(outputPath, "test-prefix")

			require.NoError(t, err)
			assert.Equal(t, outputPath, result)

			// Verify directory exists
			info, err := os.Stat(filepath.Dir(result))
			require.NoError(t, err)
			assert.True(t, info.IsDir())

			// Verify within temp directory
			assert.True(t, strings.HasPrefix(result, tempDir))
		})
	}
}
