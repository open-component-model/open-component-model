package transformation

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetermineOutputPath(t *testing.T) {
	t.Run("empty path creates an absolute temporary file", func(t *testing.T) {
		got, err := determineOutputPath("", "pypi-artifact")
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.Remove(got) })
		assert.True(t, filepath.IsAbs(got))
		assert.FileExists(t, got)
	})

	t.Run("a directory receives the temporary file", func(t *testing.T) {
		dir := t.TempDir()
		got, err := determineOutputPath(dir, "pypi-artifact")
		require.NoError(t, err)
		assert.Equal(t, dir, filepath.Dir(got))
		assert.FileExists(t, got)
	})

	t.Run("a missing directory is an error", func(t *testing.T) {
		_, err := determineOutputPath(filepath.Join(t.TempDir(), "missing"), "pypi-artifact")
		require.ErrorContains(t, err, "output path does not exist")
	})

	t.Run("a file instead of a directory is an error", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(file, nil, 0o644))
		_, err := determineOutputPath(file, "pypi-artifact")
		require.ErrorContains(t, err, "is a file, not a directory")
	})
}
