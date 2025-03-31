package filesystem_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

func TestNewFS(t *testing.T) {
	tempDir := t.TempDir()

	fsys, err := filesystem.NewFS(tempDir, os.O_RDWR)
	require.NoError(t, err)
	require.Equal(t, tempDir, fsys.Base())
}

func TestNewFS_NonExistentPath(t *testing.T) {
	tempDir := filepath.Join(t.TempDir(), "nonexistent")

	_, err := filesystem.NewFS(tempDir, os.O_RDWR)
	require.Error(t, err)
}

func TestFileSystemOperations(t *testing.T) {
	tempDir := t.TempDir()
	fsys, err := filesystem.NewFS(tempDir, os.O_RDWR)
	require.NoError(t, err)

	// Test MkdirAll
	dirPath := "testdir"
	require.NoError(t, fsys.MkdirAll(dirPath, 0755))

	// Test OpenFile
	filePath := "testdir/testfile.txt"
	file, err := fsys.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	require.NoError(t, file.Close())

	// Test ReadDir
	entries, err := fsys.ReadDir("testdir")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "testfile.txt", entries[0].Name())

	// Test Stat
	info, err := fsys.Stat(filePath)
	require.NoError(t, err)
	require.False(t, info.IsDir())

	// Test Remove
	require.NoError(t, fsys.Remove(filePath))

	// Test RemoveAll
	require.NoError(t, fsys.RemoveAll(dirPath))
}

func TestReadOnly(t *testing.T) {
	tempDir := t.TempDir()
	fsys, err := filesystem.NewFS(tempDir, os.O_RDONLY)
	require.NoError(t, err)

	// Ensure it's read-only
	require.True(t, fsys.ReadOnly())

	// Test that write operations fail
	dirPath := "testdir"
	require.ErrorIs(t, fsys.MkdirAll(dirPath, 0755), filesystem.ErrReadOnly)

	filePath := "testfile.txt"
	_, err = fsys.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0644)
	require.ErrorIs(t, err, filesystem.ErrReadOnly)

	// Test that force read only also works on an original RDWR instance
	fsys, err = filesystem.NewFS(tempDir, os.O_RDWR)
	require.NoError(t, err)
	// Force read-only mode explicitly
	fsys.ForceReadOnly()
	require.True(t, fsys.ReadOnly())

	// Test that write operations fail
	require.ErrorIs(t, fsys.MkdirAll(dirPath, 0755), filesystem.ErrReadOnly)

	_, err = fsys.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0644)
	require.ErrorIs(t, err, filesystem.ErrReadOnly)
}

func TestOpen(t *testing.T) {
	tempDir := t.TempDir()
	fsys, err := filesystem.NewFS(tempDir, os.O_RDWR)
	require.NoError(t, err)

	filePath := "testfile.txt"
	_, err = fsys.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)

	file, err := fsys.Open(filePath)
	require.NoError(t, err)
	require.NoError(t, file.Close())
}
