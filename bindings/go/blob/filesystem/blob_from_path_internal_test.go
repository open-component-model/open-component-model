package filesystem

import (
	"archive/tar"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// =============================================================================
// UTILITY FUNCTION TESTS
// Tests for helper functions that support the main functionality
// =============================================================================

func TestIsPathIncluded(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		includeFiles []string
		excludeFiles []string
		expected     bool
		expectError  bool
	}{
		{
			name:     "no_patterns_includes_all",
			path:     "a/b/c.txt",
			expected: true,
		},
		{
			name:         "exclude_exact_basename",
			path:         "file1.txt",
			excludeFiles: []string{"file1.txt"},
			expected:     false,
		},
		{
			name:         "exclude_glob_pattern",
			path:         "logs/error.log",
			excludeFiles: []string{"logs/*.log"},
			expected:     false,
		},
		{
			name:         "include_glob_allows_matching_only",
			path:         "dir/name.txt",
			includeFiles: []string{"*/name.txt"},
			expected:     true,
		},
		{
			name:         "include_glob_rejects_non_matching",
			path:         "dir/name.log",
			includeFiles: []string{"*/name.txt"},
			expected:     false,
		},
		{
			name:         "exclude_overrides_include",
			path:         "file2.txt",
			includeFiles: []string{"*.txt"},
			excludeFiles: []string{"file2.txt"},
			expected:     false,
		},
		{
			name:         "include_allows_when_not_excluded",
			path:         "file1.txt",
			includeFiles: []string{"*.txt"},
			excludeFiles: []string{"file2.txt"},
			expected:     true,
		},
		{
			name:         "nested_path_pattern_matching",
			path:         "nested/sub/n.txt",
			includeFiles: []string{"nested/*/n.txt"},
			expected:     true,
		},
		{
			name:         "invalid_pattern_returns_error",
			path:         "some/path",
			includeFiles: []string{"["},
			expectError:  true,
		},
		{
			name:         "exact_path_matching_without_leading_dot_slash",
			path:         "file.txt",
			includeFiles: []string{"file.txt"},
			expected:     true,
		},
		{
			name:         "leading_dot_slash_pattern_does_not_match",
			path:         "file.txt",
			includeFiles: []string{"./file.txt"},
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			result, err := isPathIncluded(tt.path, tt.includeFiles, tt.excludeFiles)

			if tt.expectError {
				r.Error(err)
				return
			}

			r.NoError(err)
			r.Equal(tt.expected, result)
		})
	}
}

// =============================================================================
// LOW-LEVEL TAR COMPONENT TESTS
// Tests for individual TAR creation components
// =============================================================================

func TestCreateTarHeader(t *testing.T) {
	r := require.New(t)

	// Setup: create a test file with known properties
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "test.txt")
	r.NoError(os.WriteFile(testPath, []byte("test content"), 0644))

	fi, err := os.Stat(testPath)
	r.NoError(err)

	tests := []struct {
		name               string
		reproducible       bool
		expectedNormalized bool
	}{
		{
			name:               "standard_header_preserves_timestamps",
			reproducible:       false,
			expectedNormalized: false,
		},
		{
			name:               "reproducible_header_normalizes_metadata",
			reproducible:       true,
			expectedNormalized: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			header, err := createTarHeader(fi, "", tt.reproducible)
			r.NoError(err)
			r.NotNil(header)
			r.Equal("test.txt", header.Name)

			if tt.expectedNormalized {
				r.True(header.ModTime.Equal(time.Unix(0, 0)), "expected normalized ModTime for reproducible builds")
				r.Equal(0, header.Uid, "expected normalized UID for reproducible builds")
				r.Equal(0, header.Gid, "expected normalized GID for reproducible builds")
			} else {
				r.False(header.ModTime.Equal(time.Unix(0, 0)), "expected preserved ModTime for standard builds")
			}
		})
	}
}

func TestCreateTarFromSingleFile(t *testing.T) {
	r := require.New(t)

	// Setup: create test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := "Hello, World!"
	r.NoError(os.WriteFile(testFile, []byte(testContent), 0644))

	// Create filesystem
	fs, err := NewFS(tmpDir, os.O_RDONLY)
	r.NoError(err)

	// Test TAR creation
	var tarData strings.Builder
	tw := tar.NewWriter(&tarData)

	opt := &DirOptions{Reproducible: true}
	err = createTarFromSingleFile(context.Background(), fs, "test.txt", opt, tw)
	r.NoError(err)
	r.NoError(tw.Close())

	// Verify TAR content
	tr := tar.NewReader(strings.NewReader(tarData.String()))
	header, err := tr.Next()
	r.NoError(err)
	r.Equal("test.txt", header.Name)

	content, err := io.ReadAll(tr)
	r.NoError(err)
	r.Equal(testContent, string(content))

	// Verify no more entries
	_, err = tr.Next()
	r.Equal(io.EOF, err)
}

// =============================================================================
// DIRECTORY TAR CREATION TESTS
// Tests for directory-based TAR creation with various scenarios
// =============================================================================

func TestCreateTarFromDir_BasicFunctionality(t *testing.T) {
	r := require.New(t)

	// Setup: create directory with files
	tmpDir := t.TempDir()
	r.NoError(os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content1"), 0644))
	r.NoError(os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("content2"), 0644))

	// Create nested directory
	nested := filepath.Join(tmpDir, "nested")
	r.NoError(os.Mkdir(nested, 0755))
	r.NoError(os.WriteFile(filepath.Join(nested, "nested.txt"), []byte("nested content"), 0644))

	// Create filesystem and TAR writer
	fs, err := NewFS(tmpDir, os.O_RDONLY)
	r.NoError(err)

	var tarData strings.Builder
	tw := tar.NewWriter(&tarData)

	opt := &DirOptions{Reproducible: true}
	err = createTarFromDir(context.Background(), fs, ".", opt, tw)
	r.NoError(err)
	r.NoError(tw.Close())

	// Verify all expected files are present
	tr := tar.NewReader(strings.NewReader(tarData.String()))
	foundFiles := make(map[string]string)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)

		content, err := io.ReadAll(tr)
		r.NoError(err)

		name := strings.TrimPrefix(header.Name, "./")
		foundFiles[name] = string(content)
	}

	// Verify expected files and content
	expectedFiles := map[string]string{
		"file1.txt":         "content1",
		"file2.txt":         "content2",
		"nested/nested.txt": "nested content",
	}

	for expectedFile, expectedContent := range expectedFiles {
		r.Contains(foundFiles, expectedFile, "expected file %s to be in TAR", expectedFile)
		r.Equal(expectedContent, foundFiles[expectedFile], "content mismatch for file %s", expectedFile)
	}
}

func TestCreateTarFromDir_PreserveDirectory(t *testing.T) {
	r := require.New(t)

	// Setup: create parent directory and target directory
	parentDir := t.TempDir()
	targetDirName := "target"
	targetDir := filepath.Join(parentDir, targetDirName)
	r.NoError(os.Mkdir(targetDir, 0755))
	r.NoError(os.WriteFile(filepath.Join(targetDir, "file.txt"), []byte("content"), 0644))

	// Test with PreserveDir option
	fs, err := NewFS(parentDir, os.O_RDONLY)
	r.NoError(err)

	var tarData strings.Builder
	tw := tar.NewWriter(&tarData)

	opt := &DirOptions{Reproducible: true, PreserveDir: true}
	err = createTarFromDir(context.Background(), fs, targetDirName, opt, tw)
	r.NoError(err)
	r.NoError(tw.Close())

	// Verify entries are prefixed with directory name
	tr := tar.NewReader(strings.NewReader(tarData.String()))
	foundPrefixedEntry := false

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)

		name := strings.TrimPrefix(header.Name, "./")
		if strings.HasPrefix(name, targetDirName+"/") {
			foundPrefixedEntry = true
			if strings.HasSuffix(name, "file.txt") {
				content, err := io.ReadAll(tr)
				r.NoError(err)
				r.Equal("content", string(content))
			}
		}
	}

	r.True(foundPrefixedEntry, "expected entries to be prefixed with directory name when PreserveDir=true")
}

func TestCreateTarFromDir_SymlinkRejection(t *testing.T) {
	r := require.New(t)

	// Setup: create directory with symlink
	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "target.txt")
	symlinkFile := filepath.Join(tmpDir, "link.txt")

	r.NoError(os.WriteFile(targetFile, []byte("target content"), 0644))

	if err := os.Symlink("target.txt", symlinkFile); err != nil {
		t.Skipf("symlink creation failed (may not be supported): %v", err)
		return
	}

	// Test that symlinks are rejected
	fs, err := NewFS(tmpDir, os.O_RDONLY)
	r.NoError(err)

	tw := tar.NewWriter(io.Discard)
	defer tw.Close()

	opt := &DirOptions{Reproducible: true}
	err = createTarFromDir(context.Background(), fs, ".", opt, tw)
	r.Error(err)
	r.Contains(err.Error(), "symlinks are not supported")
}

// =============================================================================
// HIGH-LEVEL STREAM CREATION TESTS
// Tests for the complete TAR stream creation functionality
// =============================================================================

func TestCreateTarStream_SingleFile(t *testing.T) {
	r := require.New(t)

	// Setup: create test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := "Test file content for stream creation"
	r.NoError(os.WriteFile(testFile, []byte(testContent), 0644))

	// Test stream creation
	opt := &DirOptions{Reproducible: true}
	reader, err := createTarStream(context.Background(), testFile, opt)
	r.NoError(err)
	r.NotNil(reader)

	// Verify TAR content through stream
	tr := tar.NewReader(reader)
	header, err := tr.Next()
	r.NoError(err)
	r.Equal("test.txt", header.Name)

	content, err := io.ReadAll(tr)
	r.NoError(err)
	r.Equal(testContent, string(content))

	// Verify no more entries
	_, err = tr.Next()
	r.Equal(io.EOF, err)
}

func TestCreateTarStream_Directory(t *testing.T) {
	r := require.New(t)

	// Setup: create directory with multiple files
	tmpDir := t.TempDir()
	r.NoError(os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content1"), 0644))
	r.NoError(os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("content2"), 0644))

	// Test stream creation for directory
	opt := &DirOptions{Reproducible: true}
	reader, err := createTarStream(context.Background(), tmpDir, opt)
	r.NoError(err)
	r.NotNil(reader)

	// Verify directory content through stream
	tr := tar.NewReader(reader)
	foundFiles := make(map[string]bool)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)

		name := strings.TrimPrefix(header.Name, "./")
		if strings.HasSuffix(name, "file1.txt") {
			foundFiles["file1.txt"] = true
		} else if strings.HasSuffix(name, "file2.txt") {
			foundFiles["file2.txt"] = true
		}

		// Consume content
		_, err = io.ReadAll(tr)
		r.NoError(err)
	}

	r.True(foundFiles["file1.txt"], "expected file1.txt in TAR stream")
	r.True(foundFiles["file2.txt"], "expected file2.txt in TAR stream")
}
