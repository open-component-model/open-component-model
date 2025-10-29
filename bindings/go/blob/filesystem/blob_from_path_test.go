package filesystem_test

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

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

// =============================================================================
// CORE FUNCTIONALITY TESTS
// Basic single file and directory blob creation
// =============================================================================

func TestGetBlobFromPath_SingleFile(t *testing.T) {
	r := require.New(t)

	// Setup: create test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := "Test blob content"
	r.NoError(os.WriteFile(testFile, []byte(testContent), 0644))

	// Test: create blob from single file (should be raw, not TAR)
	opt := filesystem.DirOptions{Reproducible: true}
	b, err := filesystem.GetBlobFromPath(context.Background(), testFile, opt)
	r.NoError(err)
	r.NotNil(b)

	// Verify: blob contains raw file content (not TAR archive)
	reader, err := b.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(reader.Close()) }()

	content, err := io.ReadAll(reader)
	r.NoError(err)
	r.Equal(testContent, string(content))
}

func TestGetBlobFromPath_Directory(t *testing.T) {
	r := require.New(t)

	// Setup: create directory with multiple files
	tmpDir := t.TempDir()
	r.NoError(os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content1"), 0644))
	r.NoError(os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("content2"), 0644))

	// Test: create blob from directory (should be TAR archive)
	opt := filesystem.DirOptions{Reproducible: true}
	b, err := filesystem.GetBlobFromPath(context.Background(), tmpDir, opt)
	r.NoError(err)
	r.NotNil(b)

	// Verify: directory structure is preserved in TAR
	reader, err := b.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(reader.Close()) }()

	tr := tar.NewReader(reader)
	foundFiles := map[string]string{}

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

	r.Equal("content1", foundFiles["file1.txt"])
	r.Equal("content2", foundFiles["file2.txt"])
}

// =============================================================================
// PATTERN FILTERING TESTS
// Include/exclude pattern functionality and directory traversal semantics
// =============================================================================

func TestGetBlobFromPath_PatternSemantics(t *testing.T) {
	r := require.New(t)

	tests := []struct {
		name            string
		includePatterns []string
		excludePatterns []string
		expectedFiles   []string
		expectError     bool
	}{
		{
			name:            "Include only go files (non-test)",
			includePatterns: []string{"*.go"},
			excludePatterns: []string{"*_test.go"},
			expectedFiles:   []string{"main.go", "helper.go"},
		},
		{
			name:            "Include files in subdirectories",
			includePatterns: []string{"config/config.json", "*.md"},
			excludePatterns: []string{},
			expectedFiles:   []string{"config/config.json", "README.md"},
		},
		{
			name:            "Exclude test directories",
			includePatterns: []string{},
			excludePatterns: []string{"test", "*.tmp"},
			expectedFiles:   []string{"main.go", "helper.go", "main_test.go", "config/config.json", "README.md"},
		},
		{
			name:            "Include and exclude combination",
			includePatterns: []string{"*.go", "config/config.json"},
			excludePatterns: []string{"*_test.go"},
			expectedFiles:   []string{"main.go", "helper.go", "config/config.json"},
		},
		{
			name:            "Exclude precedence over include",
			includePatterns: []string{"*.go"},
			excludePatterns: []string{"main.go"},
			expectedFiles:   []string{"helper.go", "main_test.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test directory structure
			tmpDir := t.TempDir()
			setupComplexTestStructure(t, tmpDir)

			// Test with patterns
			opt := filesystem.DirOptions{
				IncludePatterns: tt.includePatterns,
				ExcludePatterns: tt.excludePatterns,
				Reproducible:    true,
			}

			blob, err := filesystem.GetBlobFromPath(context.Background(), tmpDir, opt)
			if tt.expectError {
				r.Error(err)
				return
			}
			r.NoError(err)
			r.NotNil(blob)

			// Verify TAR contents
			files := extractTarContents(t, blob)
			r.ElementsMatch(tt.expectedFiles, files)
		})
	}
}

func TestGetBlobFromPath_SingleFileWithPatterns(t *testing.T) {
	r := require.New(t)

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	r.NoError(os.WriteFile(testFile, []byte("content"), 0644))

	// Test: patterns with single file should error
	opt := filesystem.DirOptions{
		IncludePatterns: []string{"*.txt"},
	}

	_, err := filesystem.GetBlobFromPath(context.Background(), testFile, opt)
	r.Error(err)
	r.Contains(err.Error(), "include/exclude patterns are not supported for single files")

	// Test: exclude patterns with single file should also error
	opt = filesystem.DirOptions{
		ExcludePatterns: []string{"*.log"},
	}

	_, err = filesystem.GetBlobFromPath(context.Background(), testFile, opt)
	r.Error(err)
	r.Contains(err.Error(), "include/exclude patterns are not supported for single files")
}

func TestGetBlobFromPath_DirectoryTraversal(t *testing.T) {
	r := require.New(t)

	// Setup: nested structure (but not too deep for filepath.Match)
	tmpDir := t.TempDir()
	createTestFile(t, tmpDir, "src/main.go", "package main")
	createTestFile(t, tmpDir, "src/helper.py", "print('hello')")
	createTestFile(t, tmpDir, "shallow.go", "package test")

	// Test: include pattern should traverse directories to find matching files
	// Note: filepath.Match doesn't support recursive patterns, so we use exact paths
	opt := filesystem.DirOptions{
		IncludePatterns: []string{"src/main.go", "shallow.go"},
		Reproducible:    true,
	}

	blob, err := filesystem.GetBlobFromPath(context.Background(), tmpDir, opt)
	r.NoError(err)

	files := extractTarContents(t, blob)
	expectedFiles := []string{"src/main.go", "shallow.go"}
	r.ElementsMatch(expectedFiles, files)
}

func TestGetBlobFromPath_ExcludeDirectoryAndContents(t *testing.T) {
	r := require.New(t)

	tmpDir := t.TempDir()
	createTestFile(t, tmpDir, "src/main.go", "package main")
	createTestFile(t, tmpDir, "src/helper.go", "package src")
	createTestFile(t, tmpDir, "test/main_test.go", "package test")
	createTestFile(t, tmpDir, "test/helper_test.go", "package test")
	createTestFile(t, tmpDir, "docs/README.md", "# README")

	// Test: exclude entire test directory
	opt := filesystem.DirOptions{
		ExcludePatterns: []string{"test"},
		Reproducible:    true,
	}

	blob, err := filesystem.GetBlobFromPath(context.Background(), tmpDir, opt)
	r.NoError(err)

	files := extractTarContents(t, blob)

	// Should contain src files and docs, but NO test files
	expectedFiles := []string{"src/main.go", "src/helper.go", "docs/README.md"}
	r.ElementsMatch(expectedFiles, files)

	// Verify test files are not included
	for _, file := range files {
		r.NotContains(file, "test/")
	}
}

// =============================================================================
// DIRECTORY STRUCTURE OPTIONS
// Tests for PreserveDir and directory handling
// =============================================================================

func TestGetBlobFromPath_PreserveDirectory(t *testing.T) {
	r := require.New(t)

	// Setup: create named directory with content
	parent := t.TempDir()
	targetDirName := "preserve_me"
	targetDir := filepath.Join(parent, targetDirName)
	r.NoError(os.Mkdir(targetDir, 0755))
	r.NoError(os.WriteFile(filepath.Join(targetDir, "file.txt"), []byte("content"), 0644))

	// Test: preserve directory structure
	opt := filesystem.DirOptions{
		Reproducible: true,
		PreserveDir:  true,
	}
	b, err := filesystem.GetBlobFromPath(context.Background(), targetDir, opt)
	r.NoError(err)

	// Verify: entries are prefixed with directory name
	reader, err := b.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(reader.Close()) }()

	tr := tar.NewReader(reader)
	foundPrefixed := false
	var foundHeaders []string

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)

		foundHeaders = append(foundHeaders, header.Name)

		// Check for directory prefix (could be with or without leading ./)
		name := strings.TrimPrefix(header.Name, "./")
		if strings.HasPrefix(name, targetDirName+"/") || strings.HasPrefix(header.Name, targetDirName+"/") {
			foundPrefixed = true
		}

		// Consume content
		_, err = io.ReadAll(tr)
		r.NoError(err)
	}

	// Debug output to understand what we got
	if !foundPrefixed {
		t.Logf("Expected prefix: %s/", targetDirName)
		t.Logf("Found headers: %v", foundHeaders)
	}

	r.True(foundPrefixed, "expected entries to be prefixed with directory name when PreserveDir=true")
}

// =============================================================================
// COMPRESSION AND MEDIA TYPE TESTS
// Tests for compression and media type handling
// =============================================================================

func TestGetBlobFromPath_Compression(t *testing.T) {
	r := require.New(t)

	// Setup: create test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	r.NoError(os.WriteFile(testFile, []byte("test content"), 0644))

	// Test: compression enabled
	opt := filesystem.DirOptions{
		Compress:  true,
		MediaType: filesystem.DefaultTarMediaType,
	}
	b, err := filesystem.GetBlobFromPath(context.Background(), testFile, opt)
	r.NoError(err)

	// Verify: content is gzip compressed
	reader, err := b.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(reader.Close()) }()

	// Check gzip magic bytes
	magicBytes := make([]byte, 2)
	_, err = io.ReadFull(reader, magicBytes)
	r.NoError(err)
	r.Equal(byte(0x1f), magicBytes[0])
	r.Equal(byte(0x8b), magicBytes[1])
}

func TestGetBlobFromPath_DefaultMediaType(t *testing.T) {
	r := require.New(t)

	// Setup: create test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	r.NoError(os.WriteFile(testFile, []byte("test content"), 0644))

	// Test: default media type when not specified
	opt := filesystem.DirOptions{Compress: false}
	b, err := filesystem.GetBlobFromPath(context.Background(), testFile, opt)
	r.NoError(err)

	// Verify: blob can be read successfully (functional test)
	reader, err := b.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(reader.Close()) }()

	content, err := io.ReadAll(reader)
	r.NoError(err)
	r.NotEmpty(content, "expected blob to contain data")
}

func TestGetBlobFromPath_MediaTypeHandling(t *testing.T) {
	r := require.New(t)

	tmpDir := t.TempDir()

	t.Run("Directory with custom media type", func(t *testing.T) {
		createTestFile(t, tmpDir, "file.txt", "content")

		opt := filesystem.DirOptions{
			MediaType: "application/custom-tar",
		}

		blob, err := filesystem.GetBlobFromPath(context.Background(), tmpDir, opt)
		r.NoError(err)

		// Note: ReadOnlyBlob interface doesn't expose MediaType,
		// so we can't directly test this, but we ensure no error occurs
		r.NotNil(blob)
	})

	t.Run("Single file with custom media type", func(t *testing.T) {
		testFile := createTestFile(t, tmpDir, "single.txt", "content")

		opt := filesystem.DirOptions{
			MediaType: "text/plain",
		}

		blob, err := filesystem.GetBlobFromPath(context.Background(), testFile, opt)
		r.NoError(err)
		r.NotNil(blob)
	})
}

// =============================================================================
// REPRODUCIBILITY TESTS
// Tests for reproducible build functionality
// =============================================================================

func TestGetBlobFromPath_ReproducibleBuilds(t *testing.T) {
	r := require.New(t)

	// Setup: create test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	r.NoError(os.WriteFile(testFile, []byte("test content"), 0644))

	// Test: create blob with reproducible option
	opt := filesystem.DirOptions{Reproducible: true}
	blob1, err := filesystem.GetBlobFromPath(context.Background(), testFile, opt)
	r.NoError(err)

	data1, err := readAllFromBlob(blob1)
	r.NoError(err)

	// Modify file timestamp
	newTime := time.Now().Add(5 * time.Minute)
	r.NoError(os.Chtimes(testFile, newTime, newTime))

	// Test: create blob again after timestamp change
	blob2, err := filesystem.GetBlobFromPath(context.Background(), testFile, opt)
	r.NoError(err)

	data2, err := readAllFromBlob(blob2)
	r.NoError(err)

	// Verify: reproducible builds produce identical output
	r.Equal(data1, data2, "expected reproducible builds to produce identical output")
}

func TestGetBlobFromPath_NonReproducibleBuilds(t *testing.T) {
	r := require.New(t)

	// Setup: create test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	r.NoError(os.WriteFile(testFile, []byte("test content"), 0644))

	// Test: non-reproducible builds preserve timestamps
	opt := filesystem.DirOptions{Reproducible: false}
	b, err := filesystem.GetBlobFromPath(context.Background(), testFile, opt)
	r.NoError(err)

	data, err := readAllFromBlob(b)
	r.NoError(err)
	r.NotEmpty(data, "expected non-empty output from non-reproducible build")
}

// =============================================================================
// ERROR HANDLING AND EDGE CASES
// Tests for error conditions and edge cases
// =============================================================================

func TestGetBlobFromPath_ErrorCases(t *testing.T) {
	tests := []struct {
		name        string
		setupFunc   func(t *testing.T) (string, filesystem.DirOptions)
		expectError bool
		errorText   string
	}{
		{
			name: "empty_path",
			setupFunc: func(t *testing.T) (string, filesystem.DirOptions) {
				return "", filesystem.DirOptions{}
			},
			expectError: true,
		},
		{
			name: "non_existent_path",
			setupFunc: func(t *testing.T) (string, filesystem.DirOptions) {
				return "/non/existent/path", filesystem.DirOptions{}
			},
			expectError: true,
		},
		{
			name: "path_outside_working_directory",
			setupFunc: func(t *testing.T) (string, filesystem.DirOptions) {
				base := t.TempDir()
				allowed := filepath.Join(base, "allowed")
				outside := filepath.Join(base, "outside")
				require.NoError(t, os.MkdirAll(allowed, 0755))
				require.NoError(t, os.MkdirAll(outside, 0755))

				testFile := filepath.Join(outside, "test.txt")
				require.NoError(t, os.WriteFile(testFile, []byte("content"), 0644))

				return testFile, filesystem.DirOptions{WorkingDir: allowed}
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			path, opt := tt.setupFunc(t)
			_, err := filesystem.GetBlobFromPath(context.Background(), path, opt)

			if tt.expectError {
				r.Error(err)
				if tt.errorText != "" {
					r.Contains(err.Error(), tt.errorText)
				}
			} else {
				r.NoError(err)
			}
		})
	}
}

func TestGetBlobFromPath_SymlinkRejection(t *testing.T) {
	r := require.New(t)

	// Setup: create directory with symlink
	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "target.txt")
	symlinkFile := filepath.Join(tmpDir, "symlink.txt")

	r.NoError(os.WriteFile(targetFile, []byte("target content"), 0644))

	if err := os.Symlink("target.txt", symlinkFile); err != nil {
		t.Skipf("symlink creation failed (may not be supported on this system): %v", err)
		return
	}

	// Test: symlinks should be rejected during blob reading
	b, err := filesystem.GetBlobFromPath(context.Background(), tmpDir, filesystem.DirOptions{})
	r.NoError(err, "blob creation should succeed initially")

	// Verify: error occurs when reading blob content
	_, err = readAllFromBlob(b)
	r.Error(err)
	r.Contains(err.Error(), "symlinks are not supported")
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// readAllFromBlob reads all content from a blob for testing purposes
func readAllFromBlob(b blob.ReadOnlyBlob) ([]byte, error) {
	rc, err := b.ReadCloser()
	if err != nil {
		return nil, err
	}
	// Read first, then close and prefer read error over close error.
	data, readErr := io.ReadAll(rc)
	closeErr := rc.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return data, nil
}

// createTestFile creates a file with content in the specified path
func createTestFile(t *testing.T, basePath, relativePath, content string) string {
	fullPath := filepath.Join(basePath, relativePath)
	dir := filepath.Dir(fullPath)

	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644))

	return fullPath
}

// setupComplexTestStructure creates a complex directory structure for pattern testing
func setupComplexTestStructure(t *testing.T, tmpDir string) {
	// Create files and directories for pattern testing
	createTestFile(t, tmpDir, "main.go", "package main")
	createTestFile(t, tmpDir, "helper.go", "package helper")
	createTestFile(t, tmpDir, "main_test.go", "package main")
	createTestFile(t, tmpDir, "config/config.json", `{"key": "value"}`)
	createTestFile(t, tmpDir, "README.md", "# Project")
	createTestFile(t, tmpDir, "temp.tmp", "temporary")

	// Create test directory
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "test"), 0755))
	createTestFile(t, tmpDir, "test/file.txt", "test content")
}

// extractTarContents extracts file names from a TAR blob for testing
func extractTarContents(t *testing.T, b blob.ReadOnlyBlob) []string {
	reader, err := b.ReadCloser()
	require.NoError(t, err)
	defer func() { require.NoError(t, reader.Close()) }()

	tr := tar.NewReader(reader)
	var files []string

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)

		// Only track regular files, not directories
		if header.Typeflag == tar.TypeReg {
			// Normalize path (remove leading ./)
			name := strings.TrimPrefix(header.Name, "./")
			files = append(files, name)
		}

		// Consume content
		_, err = io.ReadAll(tr)
		require.NoError(t, err)
	}

	return files
}
