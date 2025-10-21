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
// BASIC FUNCTIONALITY TESTS
// Core tests for GetBlobFromPath with single files and directories
// =============================================================================

func TestGetBlobFromPath_SingleFile(t *testing.T) {
	r := require.New(t)

	// Setup: create test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := "Test blob content"
	r.NoError(os.WriteFile(testFile, []byte(testContent), 0644))

	// Test: create blob from single file
	opt := filesystem.DirOptions{Reproducible: true}
	b, err := filesystem.GetBlobFromPath(context.Background(), testFile, opt)
	r.NoError(err)
	r.NotNil(b)

	// Verify: blob contains expected TAR structure
	reader, err := b.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(reader.Close()) }()

	tr := tar.NewReader(reader)
	header, err := tr.Next()
	r.NoError(err)
	r.Equal("test.txt", header.Name)

	content, err := io.ReadAll(tr)
	r.NoError(err)
	r.Equal(testContent, string(content))
}

func TestGetBlobFromPath_Directory(t *testing.T) {
	r := require.New(t)

	// Setup: create directory with multiple files
	tmpDir := t.TempDir()
	r.NoError(os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content1"), 0644))
	r.NoError(os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("content2"), 0644))

	// Test: create blob from directory
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

func TestGetBlobFromPath_NestedDirectory(t *testing.T) {
	r := require.New(t)

	// Setup: create nested directory structure
	tmpDir := t.TempDir()
	nested := filepath.Join(tmpDir, "nested")
	r.NoError(os.Mkdir(nested, 0755))
	r.NoError(os.WriteFile(filepath.Join(nested, "nested.txt"), []byte("nested content"), 0644))
	r.NoError(os.WriteFile(filepath.Join(tmpDir, "root.txt"), []byte("root content"), 0644))

	// Test: nested structure preservation
	opt := filesystem.DirOptions{Reproducible: true}
	b, err := filesystem.GetBlobFromPath(context.Background(), tmpDir, opt)
	r.NoError(err)

	// Verify: nested paths are correctly represented
	content, err := readAllFromBlob(b)
	r.NoError(err)

	tr := tar.NewReader(strings.NewReader(string(content)))
	foundNested := false
	foundRoot := false

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)

		if strings.Contains(header.Name, "nested/nested.txt") {
			foundNested = true
		}
		if strings.Contains(header.Name, "root.txt") {
			foundRoot = true
		}

		// Consume content
		_, err = io.ReadAll(tr)
		r.NoError(err)
	}

	r.True(foundNested, "expected nested file to be included")
	r.True(foundRoot, "expected root file to be included")
}

// =============================================================================
// FILTERING AND PATTERN TESTS
// Tests for include/exclude pattern functionality
// =============================================================================

func TestGetBlobFromPath_IncludePatterns(t *testing.T) {
	r := require.New(t)

	// Setup: create multiple files with different extensions
	tmpDir := t.TempDir()
	r.NoError(os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content1"), 0644))
	r.NoError(os.WriteFile(filepath.Join(tmpDir, "file2.log"), []byte("content2"), 0644))
	r.NoError(os.WriteFile(filepath.Join(tmpDir, "file3.txt"), []byte("content3"), 0644))

	// Test: include only .txt files
	opt := filesystem.DirOptions{
		Reproducible: true,
		IncludeFiles: []string{"*.txt"},
	}
	b, err := filesystem.GetBlobFromPath(context.Background(), tmpDir, opt)
	r.NoError(err)
	r.NotNil(b)

	// Verify: only .txt files are included
	reader, err := b.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(reader.Close()) }()

	tr := tar.NewReader(reader)
	foundFiles := make(map[string]bool)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)

		name := strings.TrimPrefix(header.Name, "./")
		foundFiles[name] = true

		// Consume content
		_, err = io.ReadAll(tr)
		r.NoError(err)
	}

	r.True(foundFiles["file1.txt"], "expected file1.txt to be included")
	r.True(foundFiles["file3.txt"], "expected file3.txt to be included")
	r.False(foundFiles["file2.log"], "expected file2.log to be excluded")
}

func TestGetBlobFromPath_ExcludePatterns(t *testing.T) {
	r := require.New(t)

	// Setup: create multiple files
	tmpDir := t.TempDir()
	r.NoError(os.WriteFile(filepath.Join(tmpDir, "keep.txt"), []byte("keep"), 0644))
	r.NoError(os.WriteFile(filepath.Join(tmpDir, "exclude.log"), []byte("exclude"), 0644))
	r.NoError(os.WriteFile(filepath.Join(tmpDir, "also_keep.txt"), []byte("also_keep"), 0644))

	// Test: exclude .log files
	opt := filesystem.DirOptions{
		Reproducible: true,
		ExcludeFiles: []string{"*.log"},
	}
	b, err := filesystem.GetBlobFromPath(context.Background(), tmpDir, opt)
	r.NoError(err)
	r.NotNil(b)

	// Verify: .log files are excluded
	reader, err := b.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(reader.Close()) }()

	tr := tar.NewReader(reader)
	foundFiles := make(map[string]bool)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)

		name := strings.TrimPrefix(header.Name, "./")
		foundFiles[name] = true

		// Consume content
		_, err = io.ReadAll(tr)
		r.NoError(err)
	}

	r.True(foundFiles["keep.txt"], "expected keep.txt to be included")
	r.True(foundFiles["also_keep.txt"], "expected also_keep.txt to be included")
	r.False(foundFiles["exclude.log"], "expected exclude.log to be excluded")
}

func TestGetBlobFromPath_IncludeAndExcludePrecedence(t *testing.T) {
	r := require.New(t)

	// Setup: create test files
	tmpDir := t.TempDir()
	r.NoError(os.WriteFile(filepath.Join(tmpDir, "include.txt"), []byte("include"), 0644))
	r.NoError(os.WriteFile(filepath.Join(tmpDir, "exclude_this.txt"), []byte("exclude"), 0644))
	r.NoError(os.WriteFile(filepath.Join(tmpDir, "other.log"), []byte("other"), 0644))

	// Test: include *.txt but exclude specific file
	opt := filesystem.DirOptions{
		Reproducible: true,
		IncludeFiles: []string{"*.txt"},
		ExcludeFiles: []string{"exclude_this.txt"},
	}
	b, err := filesystem.GetBlobFromPath(context.Background(), tmpDir, opt)
	r.NoError(err)
	r.NotNil(b)

	// Verify: exclude takes precedence over include
	reader, err := b.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(reader.Close()) }()

	tr := tar.NewReader(reader)
	foundFiles := make(map[string]bool)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)

		name := strings.TrimPrefix(header.Name, "./")
		foundFiles[name] = true

		// Consume content
		_, err = io.ReadAll(tr)
		r.NoError(err)
	}

	r.True(foundFiles["include.txt"], "expected include.txt to be included")
	r.False(foundFiles["exclude_this.txt"], "expected exclude_this.txt to be excluded despite include pattern")
	r.False(foundFiles["other.log"], "expected other.log to be excluded by include pattern")
}

// =============================================================================
// DIRECTORY PRESERVATION TESTS
// Tests for PreserveDir option functionality
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

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)

		name := strings.TrimPrefix(header.Name, "./")
		if strings.HasPrefix(name, targetDirName+"/") {
			foundPrefixed = true
		}

		// Consume content
		_, err = io.ReadAll(tr)
		r.NoError(err)
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
	r.NotEmpty(content, "expected blob to contain TAR data")
}

func TestGetBlobFromPath_CustomMediaType(t *testing.T) {
	r := require.New(t)

	// Setup: create test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	r.NoError(os.WriteFile(testFile, []byte("test content"), 0644))

	// Test: custom media type with compression
	customMediaType := "application/x-custom-tar"
	opt := filesystem.DirOptions{
		Compress:  true,
		MediaType: customMediaType,
	}
	b, err := filesystem.GetBlobFromPath(context.Background(), testFile, opt)
	r.NoError(err)

	// Verify: content is compressed (functional test)
	reader, err := b.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(reader.Close()) }()

	// Check gzip magic bytes to verify compression
	magicBytes := make([]byte, 2)
	_, err = io.ReadFull(reader, magicBytes)
	r.NoError(err)
	r.Equal(byte(0x1f), magicBytes[0])
	r.Equal(byte(0x8b), magicBytes[1])
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
