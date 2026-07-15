package transformation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_determineOutputPath_EmptyPath(t *testing.T) {
	prefix := "test-prefix"
	outputPath, err := determineOutputPath("", prefix)

	require.NoError(t, err)
	assert.NotEmpty(t, outputPath)
	assert.Contains(t, filepath.Base(outputPath), prefix)
	assert.True(t, filepath.IsAbs(outputPath), "expected an absolute path, got %q", outputPath)

	// Verify file exists and clean up.
	info, err := os.Stat(outputPath)
	require.NoError(t, err)
	assert.False(t, info.IsDir())
	_ = os.Remove(outputPath)
}

func Test_determineOutputPath_DirectoryPath(t *testing.T) {
	tempDir := t.TempDir()
	prefix := "test-prefix"

	outputPath, err := determineOutputPath(tempDir, prefix)

	require.NoError(t, err)
	assert.Contains(t, filepath.Base(outputPath), prefix)
	assert.True(t, strings.HasPrefix(outputPath, tempDir))
	assert.True(t, filepath.IsAbs(outputPath), "expected an absolute path, got %q", outputPath)
	// ensure the outputPath is a file, not a directory
	info, err := os.Stat(outputPath)
	require.NoError(t, err)
	assert.False(t, info.IsDir())
}

// Test_determineOutputPath_RelativeDirectoryPath verifies that a relative output
// directory still yields an absolute file path, so the returned path stays valid
// even if the working directory later changes.
func Test_determineOutputPath_RelativeDirectoryPath(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(base, "out"), 0o755))
	t.Chdir(base)

	outputPath, err := determineOutputPath("out", "test-prefix")

	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(outputPath), "expected an absolute path, got %q", outputPath)
	assert.Contains(t, filepath.Base(outputPath), "test-prefix")
	info, err := os.Stat(outputPath)
	require.NoError(t, err)
	assert.False(t, info.IsDir())
}

func Test_determineOutputPath_NonExistentPath(t *testing.T) {
	_, err := determineOutputPath(filepath.Join(t.TempDir(), "does-not-exist"), "test-prefix")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "output path does not exist")
}

func Test_determineOutputPath_ExistingFilePath(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "existing-file.tar.gz")
	require.NoError(t, os.WriteFile(filePath, []byte("content"), 0o644))

	_, err := determineOutputPath(filePath, "test-prefix")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "is a file, not a directory")
}
