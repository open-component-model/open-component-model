package inmemory

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testFile represents a file to be created in tests
type testFile struct {
	path    string
	content string
	mode    os.FileMode
}

// testDir represents a directory to be created in tests
type testDir struct {
	path string
	mode os.FileMode
}

// setupTestFS creates a filesystem with the given files and directories
func setupTestFS(t *testing.T, files []testFile, dirs []testDir) *FileSystem {
	fs := New()

	// Create directories first
	for _, dir := range dirs {
		err := fs.MkdirAll(dir.path, dir.mode)
		require.NoError(t, err, "failed to create directory %s", dir.path)
	}

	// Create files
	for _, file := range files {
		f, err := fs.OpenFile(file.path, os.O_CREATE|os.O_WRONLY, file.mode)
		require.NoError(t, err, "failed to create file %s", file.path)
		if file.content != "" {
			fileObj, ok := f.(interface{ Write([]byte) (int, error) })
			require.True(t, ok, "file does not implement Write")
			_, err = fileObj.Write([]byte(file.content))
			require.NoError(t, err, "failed to write to file %s", file.path)
		}
		f.Close()
	}

	return fs
}

// assertFileContent verifies that a file has the expected content
func assertFileContent(t *testing.T, fs *FileSystem, path, expected string) {
	t.Helper()
	f, err := fs.Open(path)
	require.NoError(t, err, "failed to open file %s", path)
	defer f.Close()

	content, err := io.ReadAll(f)
	require.NoError(t, err, "failed to read file %s", path)
	assert.Equal(t, expected, string(content), "file %s content mismatch", path)
}

// assertFileExists verifies that a file exists
func assertFileExists(t *testing.T, fs *FileSystem, path string) {
	t.Helper()
	_, err := fs.Stat(path)
	assert.NoError(t, err, "file %s should exist", path)
}

// assertFileNotExists verifies that a file does not exist
func assertFileNotExists(t *testing.T, fs *FileSystem, path string) {
	t.Helper()
	_, err := fs.Stat(path)
	assert.ErrorIs(t, err, ErrNotExist, "file %s should not exist", path)
}

// assertError verifies that an error matches the expected error
func assertError(t *testing.T, err, expected error) {
	t.Helper()
	assert.ErrorIs(t, err, expected, "error mismatch")
}

func TestNew(t *testing.T) {
	fs := New()
	require.NotNil(t, fs, "New() returned nil")
	require.NotNil(t, fs.root, "root node is nil")
	assert.True(t, fs.root.isDir, "root node should be a directory")
}

func TestOpen(t *testing.T) {
	tests := []struct {
		name          string
		setup         func() *FileSystem
		path          string
		expectedError error
		check         func(*testing.T, *FileSystem)
	}{
		{
			name: "non-existent file",
			setup: func() *FileSystem {
				return New()
			},
			path:          "nonexistent.txt",
			expectedError: ErrNotExist,
		},
		{
			name: "existing file",
			setup: func() *FileSystem {
				return setupTestFS(t, []testFile{
					{path: "test.txt", content: "test content", mode: 0644},
				}, nil)
			},
			path: "test.txt",
			check: func(t *testing.T, fs *FileSystem) {
				assertFileContent(t, fs, "test.txt", "test content")
			},
		},
		{
			name: "directory",
			setup: func() *FileSystem {
				return setupTestFS(t, nil, []testDir{
					{path: "testdir", mode: 0755},
				})
			},
			path: "testdir",
			check: func(t *testing.T, fsys *FileSystem) {
				dir, err := fsys.Open("testdir")
				require.NoError(t, err)
				defer dir.Close()

				readDirFile, ok := dir.(interface {
					ReadDir(n int) ([]fs.DirEntry, error)
				})
				require.True(t, ok, "directory does not implement ReadDir")
				entries, err := readDirFile.ReadDir(-1)
				require.NoError(t, err)
				assert.Empty(t, entries, "expected 0 entries")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := tt.setup()

			_, err := fs.Open(tt.path)
			if tt.expectedError != nil {
				assertError(t, err, tt.expectedError)
				return
			}
			require.NoError(t, err)

			if tt.check != nil {
				tt.check(t, fs)
			}
		})
	}
}

func TestOpenFile(t *testing.T) {
	fs := New()

	// Test creating new file
	f, err := fs.OpenFile("test.txt", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	file := f.(*file)
	_, err = file.Write([]byte("test content"))
	require.NoError(t, err)
	f.Close()

	// Test opening existing file
	f, err = fs.OpenFile("test.txt", os.O_RDONLY, 0644)
	require.NoError(t, err)
	defer f.Close()

	content, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "test content", string(content))

	// Test read-only filesystem
	fs.ForceReadOnly()
	_, err = fs.OpenFile("new.txt", os.O_CREATE|os.O_WRONLY, 0644)
	assert.Error(t, err, "expected error when creating file in read-only filesystem")
}

func TestMkdirAll(t *testing.T) {
	fs := New()

	// Test creating single directory
	err := fs.MkdirAll("testdir", 0755)
	require.NoError(t, err)

	// Test creating nested directories
	err = fs.MkdirAll("testdir/nested/deep", 0755)
	require.NoError(t, err)

	// Verify directories exist
	_, err = fs.Stat("testdir")
	require.NoError(t, err)
	_, err = fs.Stat("testdir/nested")
	require.NoError(t, err)
	_, err = fs.Stat("testdir/nested/deep")
	require.NoError(t, err)

	// Test creating directory where file exists
	f, err := fs.OpenFile("testdir/file.txt", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	f.Close()

	err = fs.MkdirAll("testdir/file.txt", 0755)
	assert.ErrorIs(t, err, ErrIsFile)

	// Test read-only filesystem
	fs.ForceReadOnly()
	err = fs.MkdirAll("newdir", 0755)
	assert.Error(t, err, "expected error when creating directory in read-only filesystem")
}

func TestRemove(t *testing.T) {
	fs := New()

	// Create test file
	f, err := fs.OpenFile("test.txt", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	f.Close()

	// Test removing file
	err = fs.Remove("test.txt")
	require.NoError(t, err)

	// Verify file is gone
	_, err = fs.Stat("test.txt")
	assert.ErrorIs(t, err, ErrNotExist)

	// Test removing non-existent file
	err = fs.Remove("nonexistent.txt")
	assert.ErrorIs(t, err, ErrNotExist)

	// Test removing empty directory
	err = fs.MkdirAll("emptydir", 0755)
	require.NoError(t, err)
	err = fs.Remove("emptydir")
	require.NoError(t, err, "expected no error when removing empty directory")
	_, err = fs.Stat("emptydir")
	assert.ErrorIs(t, err, ErrNotExist)

	// Test removing directory with contents
	err = fs.MkdirAll("testdir", 0755)
	require.NoError(t, err)
	f, err = fs.OpenFile("testdir/file.txt", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	f.Close()

	err = fs.Remove("testdir")
	assert.Error(t, err, "expected error when removing non-empty directory")

	// Test removing root directory
	err = fs.Remove("/")
	assert.Error(t, err, "expected error when removing root directory")

	// Test removing file directly under root
	f, err = fs.OpenFile("rootfile.txt", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	f.Close()
	err = fs.Remove("rootfile.txt")
	require.NoError(t, err, "expected no error when removing file under root")
	_, err = fs.Stat("rootfile.txt")
	assert.ErrorIs(t, err, ErrNotExist)

	// Test removing empty directory directly under root
	err = fs.MkdirAll("rootdir", 0755)
	require.NoError(t, err)
	err = fs.Remove("rootdir")
	require.NoError(t, err, "expected no error when removing empty directory under root")
	_, err = fs.Stat("rootdir")
	assert.ErrorIs(t, err, ErrNotExist)

	// Test read-only filesystem
	fs.ForceReadOnly()
	err = fs.Remove("test.txt")
	assert.Error(t, err, "expected error when removing file in read-only filesystem")
}

func TestRemoveAll(t *testing.T) {
	fs := New()

	// Create test directory structure
	err := fs.MkdirAll("testdir/nested/deep", 0755)
	require.NoError(t, err)
	f, err := fs.OpenFile("testdir/file1.txt", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	f.Close()
	f, err = fs.OpenFile("testdir/nested/file2.txt", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	f.Close()

	// Test removing directory and all contents
	err = fs.RemoveAll("testdir")
	require.NoError(t, err)

	// Verify everything is gone
	_, err = fs.Stat("testdir")
	assert.ErrorIs(t, err, ErrNotExist)

	// Test removing root directory
	err = fs.RemoveAll("/")
	assert.Error(t, err, "expected error when removing root directory")

	// Test removing file directly under root
	f, err = fs.OpenFile("rootfile.txt", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	f.Close()
	err = fs.RemoveAll("rootfile.txt")
	require.NoError(t, err, "expected no error when removing file under root")
	_, err = fs.Stat("rootfile.txt")
	assert.ErrorIs(t, err, ErrNotExist)

	// Test removing directory directly under root
	err = fs.MkdirAll("rootdir", 0755)
	require.NoError(t, err)
	f, err = fs.OpenFile("rootdir/file.txt", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	f.Close()
	err = fs.RemoveAll("rootdir")
	require.NoError(t, err, "expected no error when removing directory under root")
	_, err = fs.Stat("rootdir")
	assert.ErrorIs(t, err, ErrNotExist)

	// Test read-only filesystem
	fs.ForceReadOnly()
	err = fs.RemoveAll("testdir")
	assert.Error(t, err, "expected error when removing directory in read-only filesystem")
}

func TestReadDir(t *testing.T) {
	fs := New()

	// Create test directory structure
	err := fs.MkdirAll("testdir", 0755)
	require.NoError(t, err)
	f, err := fs.OpenFile("testdir/file1.txt", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	f.Close()
	f, err = fs.OpenFile("testdir/file2.txt", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	f.Close()
	err = fs.MkdirAll("testdir/subdir", 0755)
	require.NoError(t, err)

	// Test reading directory
	entries, err := fs.ReadDir("testdir")
	require.NoError(t, err)
	assert.Len(t, entries, 3)

	// Verify entry types
	fileCount := 0
	dirCount := 0
	for _, entry := range entries {
		if entry.IsDir() {
			dirCount++
		} else {
			fileCount++
		}
	}
	assert.Equal(t, 2, fileCount, "expected 2 files")
	assert.Equal(t, 1, dirCount, "expected 1 directory")

	// Test reading non-existent directory
	_, err = fs.ReadDir("nonexistent")
	assert.ErrorIs(t, err, ErrNotExist)

	// Test reading file as directory
	_, err = fs.ReadDir("testdir/file1.txt")
	assert.ErrorIs(t, err, ErrIsFile)
}

func TestStat(t *testing.T) {
	fs := New()

	// Test stat on non-existent file
	_, err := fs.Stat("nonexistent.txt")
	assert.ErrorIs(t, err, ErrNotExist)

	// Create test file
	fil, err := fs.OpenFile("test.txt", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	_, err = fil.(*file).Write([]byte("test content"))
	require.NoError(t, err)
	fil.Close()

	// Test stat on file
	info, err := fs.Stat("test.txt")
	require.NoError(t, err)
	assert.Equal(t, "test.txt", info.Name())
	assert.Equal(t, int64(12), info.Size())
	assert.False(t, info.IsDir())

	// Create test directory
	err = fs.MkdirAll("testdir", 0755)
	require.NoError(t, err)

	// Test stat on directory
	info, err = fs.Stat("testdir")
	require.NoError(t, err)
	assert.Equal(t, "testdir", info.Name())
	assert.True(t, info.IsDir())
}

func TestFileOperations(t *testing.T) {
	fs := New()

	// Test file creation and writing
	f, err := fs.OpenFile("test.txt", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	file := f.(*file)

	// Test writing
	_, err = file.Write([]byte("test content"))
	require.NoError(t, err)
	f.Close()

	// Test reading
	f, err = fs.Open("test.txt")
	require.NoError(t, err)
	defer f.Close()

	content, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "test content", string(content))

	// Test seeking
	seekFile, ok := f.(io.Seeker)
	require.True(t, ok, "file does not implement io.Seeker")

	_, err = seekFile.Seek(5, io.SeekStart)
	require.NoError(t, err)

	content, err = io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "content", string(content))
}

func TestConcurrentAccess(t *testing.T) {
	fs := New()
	const numGoroutines = 10
	const numOperations = 100

	// Create test directory
	err := fs.MkdirAll("testdir", 0755)
	require.NoError(t, err)

	// Run concurrent operations
	done := make(chan bool)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < numOperations; j++ {
				filename := filepath.Join("testdir", fmt.Sprintf("file_%d_%d.txt", id, j))

				// Create and write
				f, err := fs.OpenFile(filename, os.O_CREATE|os.O_WRONLY, 0644)
				if err != nil {
					t.Errorf("failed to create file: %v", err)
					continue
				}
				file := f.(*file)
				_, err = file.Write([]byte("test content"))
				if err != nil {
					t.Errorf("failed to write file: %v", err)
				}
				f.Close()

				// Read
				f, err = fs.Open(filename)
				if err != nil {
					t.Errorf("failed to open file: %v", err)
					continue
				}
				content, err := io.ReadAll(f)
				if err != nil {
					t.Errorf("failed to read file: %v", err)
				}
				assert.Equal(t, "test content", string(content))
				f.Close()

				// Remove
				err = fs.Remove(filename)
				if err != nil {
					t.Errorf("failed to remove file: %v", err)
				}
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
}

func TestFilePermissions(t *testing.T) {
	fs := New()

	// Test file with no permissions
	f, err := fs.OpenFile("noaccess.txt", os.O_CREATE|os.O_WRONLY, 0000)
	require.NoError(t, err)
	f.Close()

	// Test read on file with no read permissions
	_, err = fs.Open("noaccess.txt")
	assert.ErrorIs(t, err, os.ErrPermission)

	// Test write on file with no write permissions
	f, err = fs.OpenFile("noaccess.txt", os.O_WRONLY, 0000)
	assert.ErrorIs(t, err, os.ErrPermission)

	// Test file with read-only permissions
	f, err = fs.OpenFile("readonly.txt", os.O_CREATE|os.O_WRONLY, 0400)
	require.NoError(t, err)
	f.Close()

	// Test read on read-only file
	f, err = fs.Open("readonly.txt")
	require.NoError(t, err)
	f.Close()

	// Test write on read-only file
	f, err = fs.OpenFile("readonly.txt", os.O_WRONLY, 0400)
	assert.ErrorIs(t, err, os.ErrPermission)

	// Test file with write-only permissions
	f, err = fs.OpenFile("writeonly.txt", os.O_CREATE|os.O_WRONLY, 0200)
	require.NoError(t, err)
	f.Close()

	// Test read on write-only file
	_, err = fs.Open("writeonly.txt")
	assert.ErrorIs(t, err, os.ErrPermission)

	// Test write on write-only file
	f, err = fs.OpenFile("writeonly.txt", os.O_WRONLY, 0200)
	require.NoError(t, err)
	f.Close()

	// Test file with read-write permissions
	f, err = fs.OpenFile("readwrite.txt", os.O_CREATE|os.O_WRONLY, 0600)
	require.NoError(t, err)
	f.Close()

	// Test read on read-write file
	f, err = fs.Open("readwrite.txt")
	require.NoError(t, err)
	f.Close()

	// Test write on read-write file
	f, err = fs.OpenFile("readwrite.txt", os.O_WRONLY, 0600)
	require.NoError(t, err)
	f.Close()
}

func TestDirectoryPermissions(t *testing.T) {
	fs := New()

	// Test directory with no permissions
	err := fs.MkdirAll("noaccess", 0000)
	require.NoError(t, err)

	// Test readdir on directory with no read permissions
	_, err = fs.ReadDir("noaccess")
	assert.ErrorIs(t, err, os.ErrPermission)

	// Test directory with read-only permissions
	err = fs.MkdirAll("readonly", 0400)
	require.NoError(t, err)

	// Test readdir on read-only directory
	_, err = fs.ReadDir("readonly")
	require.NoError(t, err)

	// Test creating file in read-only directory
	_, err = fs.OpenFile("readonly/test.txt", os.O_CREATE|os.O_WRONLY, 0644)
	assert.ErrorIs(t, err, os.ErrPermission)

	// Test directory with read-write permissions
	err = fs.MkdirAll("readwrite", 0700)
	require.NoError(t, err)

	// Test readdir on read-write directory
	_, err = fs.ReadDir("readwrite")
	require.NoError(t, err)

	// Test creating file in read-write directory
	f, err := fs.OpenFile("readwrite/test.txt", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	f.Close()
}

func TestFileOperationPermissions(t *testing.T) {
	fs := New()

	// Create a file with read-write permissions
	f, err := fs.OpenFile("test.txt", os.O_CREATE|os.O_WRONLY, 0600)
	require.NoError(t, err)
	fi, ok := f.(*file)
	require.True(t, ok, "failed to cast to *file")

	// Test write with write permissions
	_, err = fi.Write([]byte("test content"))
	require.NoError(t, err)
	f.Close()

	// Open file for reading
	f, err = fs.Open("test.txt")
	require.NoError(t, err)
	fi, ok = f.(*file)
	require.True(t, ok, "failed to cast to *file")

	// Test read with read permissions
	content := make([]byte, 12)
	_, err = fi.Read(content)
	require.NoError(t, err)
	f.Close()

	// Create a new file with read-only permissions
	f, err = fs.OpenFile("readonly.txt", os.O_CREATE|os.O_WRONLY, 0400)
	require.NoError(t, err)
	f.Close()

	// Test write with read-only permissions
	f, err = fs.OpenFile("readonly.txt", os.O_WRONLY, 0400)
	assert.ErrorIs(t, err, os.ErrPermission)

	// Test read with read-only permissions
	f, err = fs.Open("readonly.txt")
	require.NoError(t, err)
	fi, ok = f.(*file)
	require.True(t, ok, "failed to cast to *file")
	_, err = fi.Write([]byte("test content"))
	assert.ErrorIs(t, err, os.ErrPermission)
	f.Close()
}

func TestUnixLikeDeletion(t *testing.T) {
	fs := New()

	// Create a test instance
	f, err := fs.OpenFile("test.txt", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	instance := f.(*file)
	_, err = instance.Write([]byte("test content"))
	require.NoError(t, err)

	// Open another instance descriptor to the same instance
	f2, err := fs.Open("test.txt")
	require.NoError(t, err)

	// Remove the instance while it has open descriptors
	err = fs.Remove("test.txt")
	require.NoError(t, err, "expected no error when removing instance with open descriptors")

	// Verify the instance is marked as deleted (new opens should fail)
	_, err = fs.Open("test.txt")
	assert.ErrorIs(t, err, ErrNotExist)

	// Verify we can still read from the open instance descriptors
	_, err = instance.Seek(0, io.SeekStart)
	require.NoError(t, err)
	content, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "test content", string(content))

	content, err = io.ReadAll(f2)
	require.NoError(t, err)
	assert.Equal(t, "test content", string(content))

	// Close first descriptor
	f.Close()

	// File should still be accessible through second descriptor
	file2, ok := f2.(io.Seeker)
	require.True(t, ok, "failed to cast to *file")
	_, err = file2.Seek(0, io.SeekStart)
	require.NoError(t, err)
	content, err = io.ReadAll(f2)
	require.NoError(t, err)
	assert.Equal(t, "test content", string(content))

	// Close second descriptor
	f2.Close()

	// Now the instance should be completely gone
	_, err = fs.Stat("test.txt")
	assert.ErrorIs(t, err, ErrNotExist)
}

func TestFileSeekAndStat(t *testing.T) {
	fs := New()

	// Create test file with content
	f, err := fs.OpenFile("test.txt", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	file := f.(*file)
	_, err = file.Write([]byte("test content for seeking"))
	require.NoError(t, err)
	f.Close()

	// Test seeking from start
	f, err = fs.Open("test.txt")
	require.NoError(t, err)
	seekFile, ok := f.(io.Seeker)
	require.True(t, ok, "file does not implement io.Seeker")

	// Test seeking from start
	pos, err := seekFile.Seek(5, io.SeekStart)
	require.NoError(t, err)
	assert.Equal(t, int64(5), pos)

	// Test seeking from current position
	pos, err = seekFile.Seek(3, io.SeekCurrent)
	require.NoError(t, err)
	assert.Equal(t, int64(8), pos)

	// Test seeking from end
	pos, err = seekFile.Seek(-6, io.SeekEnd)
	require.NoError(t, err)
	assert.Equal(t, int64(18), pos)

	// Test seeking beyond file size
	pos, err = seekFile.Seek(100, io.SeekStart)
	require.NoError(t, err)
	assert.Equal(t, int64(100), pos)

	// Test seeking with invalid whence
	_, err = seekFile.Seek(0, 999)
	assert.Error(t, err, "expected error for invalid whence")

	// Test seeking with negative offset
	_, err = seekFile.Seek(-1, io.SeekStart)
	assert.Error(t, err, "expected error for negative offset")

	// Test stat on file
	info, err := f.Stat()
	require.NoError(t, err)
	assert.Equal(t, "test.txt", info.Name())
	assert.Equal(t, int64(24), info.Size())
	assert.False(t, info.IsDir())
	assert.Equal(t, os.FileMode(0644), info.Mode())

	// Test stat on closed file
	f.Close()
	_, err = f.Stat()
	assert.ErrorIs(t, err, os.ErrClosed)
}

func TestDirFileOperations(t *testing.T) {
	fs := New()

	// Create test directory structure
	err := fs.MkdirAll("testdir", 0755)
	require.NoError(t, err)
	f, err := fs.OpenFile("testdir/file1.txt", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	f.Close()
	f, err = fs.OpenFile("testdir/file2.txt", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	f.Close()
	err = fs.MkdirAll("testdir/subdir", 0755)
	require.NoError(t, err)

	// Test opening directory
	dir, err := fs.Open("testdir")
	require.NoError(t, err)
	instance, ok := dir.(*dirFile)
	require.True(t, ok, "failed to cast to *dirFile")

	// Test Read on directory
	buf := make([]byte, 10)
	_, err = instance.Read(buf)
	assert.ErrorIs(t, err, ErrIsDir)

	// Test Write on directory
	_, err = instance.Write([]byte("test"))
	assert.ErrorIs(t, err, ErrIsDir)

	// Test ReadDir with different n values
	// For each test, we need to open a new directory handle to ensure we start from the beginning
	testReadDir := func(n int, expectedCount int, expectEOF bool) {
		dir, err := fs.Open("testdir")
		require.NoError(t, err)
		dirFile, ok := dir.(*dirFile)
		require.True(t, ok, "failed to cast to *dirFile")
		defer dirFile.Close()

		entries, err := dirFile.ReadDir(n)
		if expectEOF {
			assert.ErrorIs(t, err, io.EOF)
			return
		}
		require.NoError(t, err)
		assert.Len(t, entries, expectedCount)
	}

	// Test ReadDir with n=0 (read all)
	testReadDir(0, 3, false)

	// Test ReadDir with n=1
	testReadDir(1, 1, false)

	// Test ReadDir with n=2
	testReadDir(2, 2, false)

	// Test ReadDir with n=3
	testReadDir(3, 3, false)

	// Test ReadDir with n=4 (should return all entries, no EOF)
	testReadDir(4, 3, false)

	// Test Stat on directory
	info, err := instance.Stat()
	require.NoError(t, err)
	assert.Equal(t, "testdir", info.Name())
	assert.True(t, info.IsDir())
	assert.Equal(t, os.FileMode(0755), info.Mode()&os.ModePerm)

	// Test Stat on closed directory
	instance.Close()
	_, err = instance.Stat()
	assert.ErrorIs(t, err, os.ErrClosed)

	// Test ReadDir on closed directory
	_, err = instance.ReadDir(1)
	assert.ErrorIs(t, err, os.ErrClosed)
}

func TestModTime(t *testing.T) {
	fs := New()

	// Test directory mod time
	err := fs.MkdirAll("testdir", 0755)
	require.NoError(t, err)

	// Get initial directory mod time
	dirInfo, err := fs.Stat("testdir")
	require.NoError(t, err)
	initialDirModTime := dirInfo.ModTime()

	// Wait a bit to ensure mod time would be different
	time.Sleep(time.Millisecond * 10)

	// Create a file in the directory
	f, err := fs.OpenFile("testdir/test.txt", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	f.Close()

	// Check that directory mod time was updated
	dirInfo, err = fs.Stat("testdir")
	require.NoError(t, err)
	assert.NotEqual(t, initialDirModTime, dirInfo.ModTime(), "directory mod time should be updated after creating file")

	// Test file mod time
	fileInfo, err := fs.Stat("testdir/test.txt")
	require.NoError(t, err)
	initialFileModTime := fileInfo.ModTime()

	// Wait a bit to ensure mod time would be different
	time.Sleep(time.Millisecond * 10)

	// Modify the file
	f, err = fs.OpenFile("testdir/test.txt", os.O_WRONLY, 0644)
	require.NoError(t, err)
	_, err = f.(*file).Write([]byte("test content"))
	require.NoError(t, err)
	f.Close()

	// Check that file mod time was updated
	fileInfo, err = fs.Stat("testdir/test.txt")
	require.NoError(t, err)
	assert.True(t, fileInfo.ModTime().After(initialFileModTime), "file mod time should be updated after writing content")

	// Test mod time on non-existent file
	_, err = fs.Stat("nonexistent.txt")
	assert.ErrorIs(t, err, ErrNotExist)

	// Test mod time on closed file
	f, err = fs.Open("testdir/test.txt")
	require.NoError(t, err)
	file := f.(*file)
	file.Close()
	_, err = file.Stat()
	assert.ErrorIs(t, err, os.ErrClosed)

	// Test mod time on deleted file
	err = fs.Remove("testdir/test.txt")
	require.NoError(t, err)
	_, err = fs.Stat("testdir/test.txt")
	assert.ErrorIs(t, err, ErrNotExist)

	// Test mod time on read-only filesystem
	fs.ForceReadOnly()
	_, err = fs.OpenFile("testdir/new.txt", os.O_CREATE|os.O_WRONLY, 0644)
	assert.Error(t, err, "expected error when creating file in read-only filesystem")
}
