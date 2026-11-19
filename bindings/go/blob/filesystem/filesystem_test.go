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
	require.Equal(t, tempDir, fsys.String())
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
	require.NoError(t, fsys.MkdirAll(dirPath, 0o755))

	// Test OpenFile
	filePath := "testdir/testfile.txt"
	file, err := fsys.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0o644)
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
	require.ErrorIs(t, fsys.MkdirAll(dirPath, 0o755), filesystem.ErrReadOnly)

	filePath := "testfile.txt"
	_, err = fsys.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0o644)
	require.ErrorIs(t, err, filesystem.ErrReadOnly)

	// Test that force read only also works on an original RDWR instance
	fsys, err = filesystem.NewFS(tempDir, os.O_RDWR)
	require.NoError(t, err)
	// Force read-only mode explicitly
	fsys.ForceReadOnly()
	require.True(t, fsys.ReadOnly())

	// Test that write operations fail
	require.ErrorIs(t, fsys.MkdirAll(dirPath, 0o755), filesystem.ErrReadOnly)

	_, err = fsys.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0o644)
	require.ErrorIs(t, err, filesystem.ErrReadOnly)
}

func TestOpen(t *testing.T) {
	tempDir := t.TempDir()
	fsys, err := filesystem.NewFS(tempDir, os.O_RDWR)
	require.NoError(t, err)

	filePath := "testfile.txt"
	f, err := fsys.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	file, err := fsys.Open(filePath)
	require.NoError(t, err)
	require.NoError(t, file.Close())
}

// TestRootFileSystem_BaseReplacedBySymlink covers the confinement RootFileSystem
// promises: operations reopen base by name, so a base swapped for a symlink must
// be rejected instead of writing into the symlink target.
func TestRootFileSystem_BaseReplacedBySymlink(t *testing.T) {
	r := require.New(t)
	parent := t.TempDir()
	base := filepath.Join(parent, "ctf")
	outside := filepath.Join(parent, "outside")
	r.NoError(os.Mkdir(base, 0o755))
	r.NoError(os.Mkdir(outside, 0o755))

	fsys, err := filesystem.NewFS(base, os.O_RDWR)
	r.NoError(err)

	r.NoError(os.Rename(base, base+".moved"))
	if err := os.Symlink(outside, base); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}

	_, err = fsys.OpenFile("payload", os.O_CREATE|os.O_RDWR, 0o644)
	r.ErrorIs(err, filesystem.ErrBaseReplaced)
	r.ErrorIs(fsys.MkdirAll("blobs", 0o755), filesystem.ErrBaseReplaced)

	entries, err := os.ReadDir(outside)
	r.NoError(err)
	r.Empty(entries, "writes escaped into %s", outside)
}
