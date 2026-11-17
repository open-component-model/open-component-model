package filesystem_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

func TestBufferDir(t *testing.T) {
	t.Run("an output path wins and the temporary directory is not created", func(t *testing.T) {
		r := require.New(t)
		tempDir := filepath.Join(t.TempDir(), "temp")
		dir, err := filesystem.BufferDir("out", tempDir)
		r.NoError(err)
		r.Equal("out", dir)
		r.NoDirExists(tempDir)
	})

	t.Run("the temporary directory is used and created", func(t *testing.T) {
		r := require.New(t)
		tempDir := filepath.Join(t.TempDir(), "not", "yet", "there")
		dir, err := filesystem.BufferDir("", tempDir)
		r.NoError(err)
		r.Equal(tempDir, dir)
		r.DirExists(tempDir)
	})

	t.Run("neither set selects the default of the operating system", func(t *testing.T) {
		dir, err := filesystem.BufferDir("", "")
		require.NoError(t, err)
		require.Empty(t, dir)
	})
}
