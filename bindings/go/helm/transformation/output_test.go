package transformation_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/helm/transformation"
)

func Test_DetermineOutputPath_EmptyPath(t *testing.T) {
	prefix := "test-prefix"
	outputPath, err := transformation.DetermineOutputPath("", prefix)

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

	outputPath, err := transformation.DetermineOutputPath(tempDir, prefix)

	require.NoError(t, err)
	assert.Contains(t, filepath.Base(outputPath), prefix)
	assert.True(t, strings.HasPrefix(outputPath, tempDir))
	// ensure the outputPath is a file, not a directory
	info, err := os.Stat(outputPath)
	require.NoError(t, err)
	assert.False(t, info.IsDir())
}

func Test_DetermineOutputPath_NonExistentPath(t *testing.T) {
	_, err := transformation.DetermineOutputPath("/nonexistent/path", "test-prefix")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "output path does not exist")
}

func Test_DetermineOutputPath_ExistingFilePath(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "existing-file.tar.gz")
	require.NoError(t, os.WriteFile(filePath, []byte("content"), 0o644))

	_, err := transformation.DetermineOutputPath(filePath, "test-prefix")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "is a file, not a directory")
}
