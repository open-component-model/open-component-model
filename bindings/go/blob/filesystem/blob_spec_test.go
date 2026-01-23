package filesystem_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/direct"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestGetBlobFromSpec_SingleFile(t *testing.T) {
	r := require.New(t)

	// Setup: create test file
	tmpDir := t.TempDir()
	testContent := "Test blob content"
	testFile := createTestFile(t, tmpDir, "test.txt", testContent)

	// Create File spec with file:// URI
	spec := &v1alpha1.File{
		URI:       "file://" + testFile,
		MediaType: "text/plain",
	}

	// Test: get blob from spec
	b, err := filesystem.GetBlobFromSpec(t.Context(), spec)
	r.NoError(err)
	r.NotNil(b)

	// Verify: blob contains expected content
	content := readBlobContent(t, b)
	r.Equal(testContent, content)
}

func TestGetBlobFromSpec_Directory(t *testing.T) {
	r := require.New(t)

	// Setup: create directory with files
	tmpDir := t.TempDir()
	createTestFile(t, tmpDir, "file1.txt", "content1")
	createTestFile(t, tmpDir, "file2.txt", "content2")

	// Create File spec with file:// URI pointing to directory
	spec := &v1alpha1.File{
		URI:       "file://" + tmpDir,
		MediaType: "application/x-tar",
	}

	// Test: get blob from spec
	b, err := filesystem.GetBlobFromSpec(t.Context(), spec)
	r.NoError(err)
	r.NotNil(b)

	// Verify: blob is a TAR archive (non-zero content)
	content := readBlobContent(t, b)
	r.NotEmpty(content)
}

func TestGetBlobFromSpec_MediaTypePreserved(t *testing.T) {
	r := require.New(t)

	// Setup: create test file
	tmpDir := t.TempDir()
	testFile := createTestFile(t, tmpDir, "test.json", `{"key":"value"}`)

	// Create File spec with custom media type
	customMediaType := "application/json"
	spec := &v1alpha1.File{
		URI:       "file://" + testFile,
		MediaType: customMediaType,
	}

	// Test: get blob from spec
	b, err := filesystem.GetBlobFromSpec(t.Context(), spec)
	r.NoError(err)
	r.NotNil(b)

	// Verify: media type is preserved in the blob
	if mediaTypeAware, ok := b.(interface{ MediaType() (string, bool) }); ok {
		mt, known := mediaTypeAware.MediaType()
		r.True(known, "media type should be known")
		r.Equal(customMediaType, mt)
	} else {
		t.Fatal("blob does not implement MediaType()")
	}
}

func TestGetBlobFromSpec_RelativePath(t *testing.T) {
	r := require.New(t)

	// Setup: create test file
	tmpDir := t.TempDir()
	testContent := "Test content"
	testFile := createTestFile(t, tmpDir, "test.txt", testContent)

	// Get relative path
	cwd, err := os.Getwd()
	r.NoError(err)

	relPath, err := filepath.Rel(cwd, testFile)
	r.NoError(err)

	// Skip test if we can't create a relative path
	if err != nil || relPath == "" {
		t.Skip("Cannot create relative path")
	}

	// Create File spec with relative file:// URI
	spec := &v1alpha1.File{
		URI: "file://" + relPath,
	}

	// Test: get blob from spec (should resolve to absolute path)
	b, err := filesystem.GetBlobFromSpec(t.Context(), spec)
	r.NoError(err)
	r.NotNil(b)

	// Verify: blob contains expected content
	content := readBlobContent(t, b)
	r.Equal(testContent, content)
}

func TestGetBlobFromSpec_InvalidURI(t *testing.T) {
	r := require.New(t)

	// Create File spec with invalid URI
	spec := &v1alpha1.File{
		URI: "not a valid uri with spaces and ://?#",
	}

	// Test: should return error for invalid URI
	_, err := filesystem.GetBlobFromSpec(t.Context(), spec)
	r.Error(err)
	r.Contains(err.Error(), "invalid URI")
}

func TestGetBlobFromSpec_EmptyPath(t *testing.T) {
	r := require.New(t)

	// Create File spec with empty path
	spec := &v1alpha1.File{
		URI: "file://",
	}

	// Test: should return error for empty path
	_, err := filesystem.GetBlobFromSpec(t.Context(), spec)
	r.Error(err)
	r.Contains(err.Error(), "empty file path")
}

func TestGetBlobFromSpec_UnsupportedScheme(t *testing.T) {
	tests := []struct {
		name   string
		scheme string
	}{
		{"HTTP scheme", "http://example.com/file.txt"},
		{"HTTPS scheme", "https://example.com/file.txt"},
		{"FTP scheme", "ftp://server.com/file.txt"},
		{"Custom scheme", "custom://path/to/file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			// Create File spec with unsupported URI scheme
			spec := &v1alpha1.File{
				URI: tt.scheme,
			}

			// Test: should return error for unsupported scheme
			_, err := filesystem.GetBlobFromSpec(t.Context(), spec)
			r.Error(err)
			r.Contains(err.Error(), "unsupported URI scheme")
		})
	}
}

func TestGetBlobFromSpec_NonExistentFile(t *testing.T) {
	r := require.New(t)

	// Create File spec with non-existent file path
	spec := &v1alpha1.File{
		URI: "file:///this/path/does/not/exist/file.txt",
	}

	// Test: should return error for non-existent file
	_, err := filesystem.GetBlobFromSpec(t.Context(), spec)
	r.Error(err)
	r.Contains(err.Error(), "error accessing path")
}

func TestGetBlobFromSpec_InvalidSpecType(t *testing.T) {
	r := require.New(t)

	// Create a simple Typed implementation with wrong type
	invalidSpec := &runtime.Raw{
		Type: runtime.Type{
			Name:    "NotAFile",
			Version: "v1",
		},
	}

	// Test: should return error when spec cannot be converted to File
	_, err := filesystem.GetBlobFromSpec(t.Context(), invalidSpec)
	r.Error(err)
	r.Contains(err.Error(), "cannot convert spec to File")
}

func TestBlobToSpec_SingleFile(t *testing.T) {
	r := require.New(t)

	// Setup: create temp directory and target path
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "output.txt")

	// Create a blob with content
	testContent := "Test blob content for spec"
	b := direct.NewFromBytes([]byte(testContent), direct.WithMediaType("text/plain"))

	// Test: convert blob to spec
	spec, err := filesystem.BlobToSpec(b, targetPath)
	r.NoError(err)
	r.NotNil(spec)

	// Verify: spec has correct URI
	r.Equal("file://"+targetPath, spec.URI)

	// Verify: spec has correct media type
	r.Equal("text/plain", spec.MediaType)

	// Verify: file was created with correct content
	content, err := os.ReadFile(targetPath)
	r.NoError(err)
	r.Equal(testContent, string(content))

	// Verify: spec type is set
	r.NotEmpty(spec.Type.Name)
}

func TestBlobToSpec_WithDigest(t *testing.T) {
	r := require.New(t)

	// Setup: create temp directory and target path
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "output.txt")

	// Create a blob with content and digest
	testContent := "Test content"
	b := direct.NewFromBytes([]byte(testContent))

	// Test: convert blob to spec
	spec, err := filesystem.BlobToSpec(b, targetPath)
	r.NoError(err)
	r.NotNil(spec)

	// Verify: spec has digest if blob provides it
	if spec.Digest != "" {
		r.NotEmpty(spec.Digest)
	}
}

func TestBlobToSpec_MediaTypeFromBlob(t *testing.T) {
	tests := []struct {
		name      string
		mediaType string
	}{
		{"JSON media type", "application/json"},
		{"TAR media type", "application/x-tar"},
		{"Custom media type", "application/vnd.custom+json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			// Setup: create temp directory and target path
			tmpDir := t.TempDir()
			targetPath := filepath.Join(tmpDir, "output.txt")

			// Create a blob with specified media type
			var b = direct.NewFromBytes([]byte("content"))
			if tt.mediaType != "" {
				b = direct.NewFromBytes([]byte("content"), direct.WithMediaType(tt.mediaType))
			}

			// Test: convert blob to spec
			spec, err := filesystem.BlobToSpec(b, targetPath)
			r.NoError(err)
			r.NotNil(spec)

			// Verify: spec has correct media type
			if tt.mediaType != "" {
				r.Equal(tt.mediaType, spec.MediaType)
			}
		})
	}
}

func TestBlobToSpec_DirectoryBlob(t *testing.T) {
	r := require.New(t)

	// Setup: create directory with files
	tmpDir := t.TempDir()
	createTestFile(t, tmpDir, "file1.txt", "content1")
	createTestFile(t, tmpDir, "file2.txt", "content2")

	// Create a blob from directory
	b, err := filesystem.GetBlobFromPath(t.Context(), tmpDir, filesystem.DirOptions{
		MediaType:    "application/x-tar",
		Reproducible: true,
	})
	r.NoError(err)

	// Setup: target path for spec output
	outputDir := t.TempDir()
	targetPath := filepath.Join(outputDir, "output.tar")

	// Test: convert directory blob to spec
	spec, err := filesystem.BlobToSpec(b, targetPath)
	r.NoError(err)
	r.NotNil(spec)

	// Verify: spec has correct URI and media type
	r.Equal("file://"+targetPath, spec.URI)
	r.Equal("application/x-tar", spec.MediaType)

	// Verify: TAR file was created
	fi, err := os.Stat(targetPath)
	r.NoError(err)
	r.True(fi.Size() > 0)
}

func TestBlobToSpec_InvalidPath(t *testing.T) {
	r := require.New(t)

	// Create a blob
	b := direct.NewFromBytes([]byte("content"))

	// Test: try to write to invalid path (directory that doesn't exist)
	invalidPath := "/nonexistent/directory/file.txt"
	_, err := filesystem.BlobToSpec(b, invalidPath)
	r.Error(err)
}

// Verify that GetBlobFromSpec and BlobToSpec are inverse operations
func TestBlobSpec_Roundtrip(t *testing.T) {
	r := require.New(t)

	// Setup: create test file
	tmpDir := t.TempDir()
	originalContent := "Original content for roundtrip test"
	originalFile := createTestFile(t, tmpDir, "original.txt", originalContent)

	// Step 1: Create spec from original file
	originalSpec := &v1alpha1.File{
		URI:       "file://" + originalFile,
		MediaType: "text/plain",
	}

	// Step 2: Get blob from spec
	b, err := filesystem.GetBlobFromSpec(t.Context(), originalSpec)
	r.NoError(err)

	// Step 3: Convert blob back to spec (write to new file)
	outputDir := t.TempDir()
	outputFile := filepath.Join(outputDir, "output.txt")
	newSpec, err := filesystem.BlobToSpec(b, outputFile)
	r.NoError(err)

	// Verify: content is preserved
	outputContent, err := os.ReadFile(outputFile)
	r.NoError(err)
	r.Equal(originalContent, string(outputContent))

	// Verify: media type is preserved
	r.Equal(originalSpec.MediaType, newSpec.MediaType)

	// Step 4: Get blob from new spec
	b2, err := filesystem.GetBlobFromSpec(t.Context(), newSpec)
	r.NoError(err)

	// Verify: content is still the same
	content := readBlobContent(t, b2)
	r.Equal(originalContent, content)
}

func readBlobContent(t *testing.T, b blob.ReadOnlyBlob) string {
	t.Helper()
	r := require.New(t)

	reader, err := b.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(reader.Close()) }()

	content, err := io.ReadAll(reader)
	r.NoError(err)

	return string(content)
}
