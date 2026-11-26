package filesystem_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

func TestBufferFile(t *testing.T) {
	t.Run("an output path wins and the temporary directory is not created", func(t *testing.T) {
		r := require.New(t)
		outputPath := t.TempDir()
		tempDir := filepath.Join(t.TempDir(), "temp")

		path, err := filesystem.BufferFile(outputPath, tempDir, "buffer")
		r.NoError(err)
		r.Equal(outputPath, filepath.Dir(path))
		r.Contains(filepath.Base(path), "buffer-")
		r.FileExists(path)
		r.NoDirExists(tempDir)
	})

	t.Run("the temporary directory is used and created", func(t *testing.T) {
		r := require.New(t)
		tempDir := filepath.Join(t.TempDir(), "not", "yet", "there")

		path, err := filesystem.BufferFile("", tempDir, "buffer")
		r.NoError(err)
		r.Equal(tempDir, filepath.Dir(path))
		r.FileExists(path)
	})

	t.Run("neither set selects the default of the operating system", func(t *testing.T) {
		r := require.New(t)

		path, err := filesystem.BufferFile("", "", "buffer")
		r.NoError(err)
		t.Cleanup(func() { _ = os.Remove(path) })
		r.Equal(strings.TrimSuffix(os.TempDir(), string(os.PathSeparator)), filepath.Dir(path))
		r.FileExists(path)
	})

	t.Run("the returned path is absolute", func(t *testing.T) {
		r := require.New(t)
		t.Chdir(t.TempDir())

		path, err := filesystem.BufferFile("", "relative-temp", "buffer")
		r.NoError(err)
		r.True(filepath.IsAbs(path))
		r.FileExists(path)
	})

	t.Run("an output path that does not exist is an error", func(t *testing.T) {
		_, err := filesystem.BufferFile(filepath.Join(t.TempDir(), "missing"), "", "buffer")
		require.ErrorContains(t, err, "output path does not exist")
	})

	t.Run("an output path that is a file is an error", func(t *testing.T) {
		r := require.New(t)
		file := filepath.Join(t.TempDir(), "file")
		r.NoError(os.WriteFile(file, []byte("content"), 0o600))

		_, err := filesystem.BufferFile(file, "", "buffer")
		r.ErrorContains(err, "is a file, not a directory")
	})
}
