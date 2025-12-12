package filesystem_test

import (
	"archive/tar"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

// CORE FUNCTIONALITY: Basic single file and directory blob creation
func TestGetBlobFromPath_SingleFile(t *testing.T) {
	r := require.New(t)

	// Setup: create test file
	tmpDir := t.TempDir()
	testContent := "Test blob content"
	testFile := createTestFile(t, tmpDir, "test.txt", testContent)

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

func TestGetBlobFromPath_SimpleDirectory(t *testing.T) {
	r := require.New(t)

	// Setup: create directory with multiple files
	tmpDir := t.TempDir()
	createTestFile(t, tmpDir, "file1.txt", "content1")
	createTestFile(t, tmpDir, "file2.txt", "content2")

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

		name := header.Name
		foundFiles[name] = string(content)
	}

	r.Equal("content1", foundFiles["file1.txt"])
	r.Equal("content2", foundFiles["file2.txt"])
}

// PATTERN FILTERING: Include/exclude pattern functionality
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
			name:            "Include only go files, exclude test files",
			includePatterns: []string{"*.go"},
			excludePatterns: []string{"*_test.go"},
			expectedFiles:   []string{"main.go", "helper.go"},
		},
		{
			name:            "Include files in subdirectory",
			includePatterns: []string{"config/my-config.json", "*.md"},
			excludePatterns: []string{},
			expectedFiles:   []string{"config/my-config.json", "README.md"},
		},
		{
			name:            "Exclude test directory and tmp files",
			includePatterns: []string{},
			excludePatterns: []string{"test", "*.tmp"},
			expectedFiles:   []string{"main.go", "helper.go", "main_test.go", "config/my-config.json", "README.md"},
		},
		{
			name:            "Combine includes and excludes",
			includePatterns: []string{"*.go", "config/my-config.json"},
			excludePatterns: []string{"*_test.go"},
			expectedFiles:   []string{"main.go", "helper.go", "config/my-config.json"},
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
			// Setup
			tmpDir := t.TempDir()
			createTestFile(t, tmpDir, "main.go", "package main")
			createTestFile(t, tmpDir, "helper.go", "package helper")
			createTestFile(t, tmpDir, "main_test.go", "package main")
			createTestFile(t, tmpDir, "config/my-config.json", `{"key": "value"}`)
			createTestFile(t, tmpDir, "README.md", "# Project")
			createTestFile(t, tmpDir, "temp.tmp", "temporary")
			require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "test"), 0755))
			createTestFile(t, tmpDir, "test/file.txt", "test content")

			// Test with patterns
			opt := filesystem.DirOptions{IncludePatterns: tt.includePatterns, ExcludePatterns: tt.excludePatterns, Reproducible: true}
			resultBlob, err := filesystem.GetBlobFromPath(context.Background(), tmpDir, opt)
			if tt.expectError {
				r.Error(err)
				return
			}
			r.NoError(err)
			r.NotNil(resultBlob)

			// Verify TAR contents
			files := extractTarContents(t, resultBlob)
			r.ElementsMatch(tt.expectedFiles, files)
		})
	}
}

func TestGetBlobFromPath_SingleFileWithPatterns(t *testing.T) {
	r := require.New(t)

	tmpDir := t.TempDir()
	testFile := createTestFile(t, tmpDir, "test.txt", "content")

	// Test: patterns with single file should error
	opt := filesystem.DirOptions{IncludePatterns: []string{"*.txt"}}
	_, err := filesystem.GetBlobFromPath(context.Background(), testFile, opt)
	r.Error(err)
	r.Contains(err.Error(), "include/exclude patterns are not supported for single files")

	// Test: exclude patterns with single file should also error
	opt = filesystem.DirOptions{ExcludePatterns: []string{"*.log"}}
	_, err = filesystem.GetBlobFromPath(context.Background(), testFile, opt)
	r.Error(err)
	r.Contains(err.Error(), "include/exclude patterns are not supported for single files")
}

// DIRECTORY STRUCTURE OPTIONS
func TestGetBlobFromPath_PreserveDirectory(t *testing.T) {
	r := require.New(t)

	// Setup: create named directory with content
	parent := t.TempDir()
	targetDirName := "preserve_me"
	targetDir := filepath.Join(parent, targetDirName)
	r.NoError(os.Mkdir(targetDir, 0755))
	createTestFile(t, targetDir, "file.txt", "content")

	// Test: preserve directory structure
	opt := filesystem.DirOptions{Reproducible: true, PreserveDir: true}
	b, err := filesystem.GetBlobFromPath(context.Background(), targetDir, opt)
	r.NoError(err)

	// Verify: entries are prefixed with directory name
	reader, err := b.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(reader.Close()) }()

	tr := tar.NewReader(reader)
	foundPrefixed := false
	var foundHeaders []string
	expectedDirHeader := filepath.Base(targetDir) + "/"

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)

		foundHeaders = append(foundHeaders, header.Name)

		// Expect exact directory header for preserved directory in canonical form (base + "/")
		if header.Typeflag == tar.TypeDir && header.Name == expectedDirHeader {
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

// COMPRESSION AND MEDIA TYPE
func TestGetBlobFromPath_Compression(t *testing.T) {
	r := require.New(t)

	// Setup: create test file
	tmpDir := t.TempDir()
	testFile := createTestFile(t, tmpDir, "test.txt", "test content")

	// Test: compression enabled
	opt := filesystem.DirOptions{Compress: true, MediaType: filesystem.DefaultTarMediaType}
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

func TestGetBlobFromPath_MediaTypeHandling(t *testing.T) {
	r := require.New(t)

	tmpDir := t.TempDir()

	t.Run("Directory with custom media type", func(t *testing.T) {
		createTestFile(t, tmpDir, "file.txt", "content")

		opt := filesystem.DirOptions{MediaType: "application/custom-tar"}
		b, err := filesystem.GetBlobFromPath(context.Background(), tmpDir, opt)
		r.NoError(err)

		mt, ok := b.(blob.MediaTypeAware)
		r.True(ok)
		media, known := mt.MediaType()
		r.True(known)
		r.Equal("application/custom-tar", media)
	})

	t.Run("Single file with custom media type", func(t *testing.T) {
		testFile := createTestFile(t, tmpDir, "single.txt", "content")

		opt := filesystem.DirOptions{MediaType: "text/plain"}
		b, err := filesystem.GetBlobFromPath(context.Background(), testFile, opt)
		r.NoError(err)

		mt, ok := b.(blob.MediaTypeAware)
		r.True(ok)
		media, known := mt.MediaType()
		r.True(known)
		r.Equal("text/plain", media)
	})
}

// REPRODUCIBILITY
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

// ERROR HANDLING & EDGE CASES
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

func TestGetBlobFromPath_IncludeDirectoryOnly(t *testing.T) {
	r := require.New(t)

	// Setup: create directory with an empty sub directory
	tmpDir := t.TempDir()
	targetDir := filepath.Join(tmpDir, "sub", "dir")
	r.NoError(os.MkdirAll(targetDir, 0755))

	// Only include the directory itself
	opt := filesystem.DirOptions{IncludePatterns: []string{"sub/dir"}, Reproducible: true}
	b, err := filesystem.GetBlobFromPath(context.Background(), tmpDir, opt)
	r.NoError(err)
	r.NotNil(b)

	reader, err := b.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(reader.Close()) }()

	tr := tar.NewReader(reader)
	foundDir := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)

		if h.Typeflag == tar.TypeDir && h.Name == "sub/dir/" {
			foundDir = true
		}
		_, err = io.ReadAll(tr)
		r.NoError(err)
	}
	r.True(foundDir, "expected directory header for sub/dir to be present when included explicitly")
}

func TestGetBlobFromPath_PatternNormalization(t *testing.T) {
	r := require.New(t)

	tmpDir := t.TempDir()
	createTestFile(t, tmpDir, "sub/dir/file.txt", "content")

	cases := [][]string{
		{"./sub/dir/*"},
		{"/sub/dir/*"},
		{"sub/dir/*"},
	}

	for _, inc := range cases {
		opt := filesystem.DirOptions{IncludePatterns: inc, Reproducible: true}
		resultBlob, err := filesystem.GetBlobFromPath(context.Background(), tmpDir, opt)
		r.NoError(err)
		files := extractTarContents(t, resultBlob)
		r.Contains(files, "sub/dir/file.txt")
	}
}

// HELPERS
func readAllFromBlob(b blob.ReadOnlyBlob) ([]byte, error) {
	rc, err := b.ReadCloser()
	if err != nil {
		return nil, err
	}
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

func createTestFile(t *testing.T, basePath, relativePath, content string) string {
	fullPath := filepath.Join(basePath, relativePath)
	dir := filepath.Dir(fullPath)

	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644))

	return fullPath
}

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
			name := header.Name
			files = append(files, name)
		}

		// Consume content
		_, err = io.ReadAll(tr)
		require.NoError(t, err)
	}

	return files
}
