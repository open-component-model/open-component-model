package dir_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/input/dir"
	v1 "ocm.software/open-component-model/bindings/go/input/dir/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestGetV1DirBlob_Success(t *testing.T) {
	type TestFile struct {
		relPath       string
		content       string
		expectedInTar bool
	}
	tests := []struct {
		name           string
		mediaType      string
		compress       bool
		preserveDir    bool
		followSymlinks bool
		excludeFiles   []string
		includeFiles   []string
		expectGzip     bool
		testDirBase    string
		testFiles      []TestFile
	}{
		{
			name:           "default dir spec with nested folders",
			mediaType:      "application/vnd.gardener.landscaper.blueprint.v1+tar",
			compress:       false,
			preserveDir:    false,
			followSymlinks: false,
			excludeFiles:   []string{},
			includeFiles:   []string{},
			expectGzip:     false,
			testDirBase:    "input-dir",
			testFiles: []TestFile{
				{relPath: "blueprint.yaml", content: "blueprint", expectedInTar: true},
				{relPath: "sub/deploy-execution.yaml", content: "deploy-execution", expectedInTar: true},
				{relPath: "sub/sub2/export-execution.yaml", content: "export-execution", expectedInTar: true},
			},
		},
		{
			name:           "compressed dir",
			mediaType:      "application/vnd.gardener.landscaper.blueprint.v1+tar",
			compress:       true,
			preserveDir:    false,
			followSymlinks: false,
			excludeFiles:   []string{},
			includeFiles:   []string{},
			expectGzip:     true,
			testDirBase:    "input-dir",
			testFiles: []TestFile{
				{relPath: "blueprint.yaml", content: "blueprint", expectedInTar: true},
			},
		},
		{
			name:           "preserve root folder",
			mediaType:      "application/vnd.gardener.landscaper.blueprint.v1+tar",
			compress:       false,
			preserveDir:    true,
			followSymlinks: false,
			excludeFiles:   []string{},
			includeFiles:   []string{},
			expectGzip:     false,
			testDirBase:    "input-dir",
			testFiles: []TestFile{
				{relPath: "sub/sub2/export-execution.yaml", content: "export-execution", expectedInTar: true},
			},
		},
		{
			name:           "exclusion of files",
			mediaType:      "application/vnd.gardener.landscaper.blueprint.v1+tar",
			compress:       false,
			preserveDir:    false,
			followSymlinks: false,
			excludeFiles:   []string{"sub/*.txt", "sub/sub2/?ile.yaml", "sub3/*"},
			includeFiles:   []string{},
			expectGzip:     false,
			testDirBase:    "input-dir",
			testFiles: []TestFile{
				{relPath: "blueprint.yaml", content: "", expectedInTar: true},
				{relPath: "sub/text.txt", content: "", expectedInTar: false}, // Excluded by "sub/*.txt".
				{relPath: "sub/yaml.yaml", content: "", expectedInTar: true},
				{relPath: "sub/sub2/file.yaml", content: "", expectedInTar: false}, // Excluded by "sub/sub2/?ile.yaml".
				{relPath: "sub/sub2/file.txt", content: "", expectedInTar: true},
				{relPath: "sub3/file.txt", content: "", expectedInTar: false},  // Excluded by "sub3/*".
				{relPath: "sub3/file.yaml", content: "", expectedInTar: false}, // Excluded by "sub3/*".
			},
		},
		{
			name:           "inclusion of files",
			mediaType:      "application/vnd.gardener.landscaper.blueprint.v1+tar",
			compress:       false,
			preserveDir:    false,
			followSymlinks: false,
			excludeFiles:   []string{},
			includeFiles:   []string{"sub", "sub/*.txt"}, // "sub" to walk into the folder and "sub/*.txt" to filter out all .txt files there.
			expectGzip:     false,
			testDirBase:    "input-dir",
			testFiles: []TestFile{
				{relPath: "blueprint.yaml", content: "", expectedInTar: false}, // Not included because path does not match to defined explicit include patterns.
				{relPath: "sub/text.txt", content: "", expectedInTar: true},    // Included by "sub/*.txt".
				{relPath: "sub/yaml.yaml", content: "", expectedInTar: false},  // Not included because path does not match to defined explicit include patterns.
			},
		},
		{
			name:           "precedence of exclusion over inclusion",
			mediaType:      "application/vnd.gardener.landscaper.blueprint.v1+tar",
			compress:       false,
			preserveDir:    false,
			followSymlinks: false,
			excludeFiles:   []string{"sub/*"},
			includeFiles:   []string{"blueprint.yaml", "sub", "sub/*.txt"},
			expectGzip:     false,
			testDirBase:    "input-dir",
			testFiles: []TestFile{
				{relPath: "blueprint.yaml", content: "", expectedInTar: true}, // Included by "blueprint.yaml".
				{relPath: "sub/text.txt", content: "", expectedInTar: false},  // Excluded by "sub/*", despite inclusion by "sub/*.txt".
				{relPath: "sub/yaml.yaml", content: "", expectedInTar: false}, // Excluded by "sub/*".
			},
		},
	}

	ctx := t.Context()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create directory structure to test with.
			tempDir := t.TempDir()
			dirAbs := filepath.Join(tempDir, tt.testDirBase)
			for _, tf := range tt.testFiles {
				filePath := filepath.Join(dirAbs, tf.relPath)
				err := os.MkdirAll(filepath.Dir(filePath), 0o755)
				require.NoError(t, err)
				err = os.WriteFile(filePath, []byte(tf.content), 0o644)
				require.NoError(t, err)
			}

			// Create v1.File spec.
			dirSpec := v1.Dir{
				Type:           runtime.NewUnversionedType("dir"),
				Path:           dirAbs,
				MediaType:      tt.mediaType,
				Compress:       tt.compress,
				PreserveDir:    tt.preserveDir,
				FollowSymlinks: tt.followSymlinks,
				ExcludeFiles:   tt.excludeFiles,
				IncludeFiles:   tt.includeFiles,
			}

			// Get blob.
			b, err := dir.GetV1DirBlob(ctx, dirSpec)
			require.NoError(t, err)
			require.NotNil(t, b)

			// Test blob properties.
			if sizeAware, ok := b.(blob.SizeAware); ok {
				size := sizeAware.Size()
				assert.GreaterOrEqual(t, size, int64(0))
			}

			if digestAware, ok := b.(blob.DigestAware); ok {
				digest, ok := digestAware.Digest()
				assert.True(t, ok)
				assert.NotEmpty(t, digest)
			}

			// Test reading data.
			reader, err := b.ReadCloser()
			require.NoError(t, err)
			defer reader.Close()

			data, err := io.ReadAll(reader)
			require.NoError(t, err)

			if tt.expectGzip {
				// Decompress gzipped data.
				gzReader, err := gzip.NewReader(bytes.NewReader(data))
				require.NoError(t, err)
				defer gzReader.Close()

				data, err = io.ReadAll(gzReader)
				require.NoError(t, err)

				// Test media type for compressed blob.
				if mediaTypeAware, ok := b.(blob.MediaTypeAware); ok {
					mediaType, known := mediaTypeAware.MediaType()
					assert.True(t, known)
					assert.Equal(t, tt.mediaType+"+gzip", mediaType)
				}
			} else {
				// Test media type for uncompressed blob.
				if mediaTypeAware, ok := b.(blob.MediaTypeAware); ok {
					mediaType, known := mediaTypeAware.MediaType()
					assert.True(t, known)
					assert.Equal(t, tt.mediaType, mediaType)
				}
			}

			// Extract files from tar archive and compare content with original files.
			for _, tf := range tt.testFiles {
				fileName := tf.relPath
				if tt.preserveDir {
					fileName = filepath.Join(tt.testDirBase, fileName)
				}

				untarredData, err := extractFileFromTar(data, fileName)
				if tf.expectedInTar {
					// If the file should have been included in the tar, check if it is there.
					require.NoError(t, err)
					assert.Equal(t, tf.content, string(untarredData))
				} else {
					// If the file should NOT have been included, an error is expected when trying to extract it.
					assert.Error(t, err)
				}
			}
		})
	}
}

func TestGetV1DirBlob_EmptyPath(t *testing.T) {
	// Create v1.Dir spec with empty path.
	dirSpec := v1.Dir{
		Type: runtime.NewUnversionedType("dir"),
		Path: "",
	}

	// Get blob should fail.
	ctx := t.Context()
	dirBlob, err := dir.GetV1DirBlob(ctx, dirSpec)
	assert.Error(t, err)
	assert.Nil(t, dirBlob)
}

func TestGetV1DirBlob_NonExistentPath(t *testing.T) {
	// Create v1.Dir spec with non-existing path.
	dirSpec := v1.Dir{
		Type: runtime.NewUnversionedType("dir"),
		Path: "/non/existent/path",
	}

	// Get blob should fail. The error:
	// "failed to create filesystem while trying to access <path>: path does not exist: <path>".
	ctx := t.Context()
	dirBlob, err := dir.GetV1DirBlob(ctx, dirSpec)
	assert.Error(t, err)
	assert.Nil(t, dirBlob)

	// Another case: the input directory does not exist, but its parent folder does.
	// In this case, and if PreserveDir is true, the FileSystem instance is created for the existing parent folder.
	// Still, as there is nothing to tar, we expect an error.
	tempDir := t.TempDir()
	dirSpec = v1.Dir{
		Type:        runtime.NewUnversionedType("dir"),
		Path:        filepath.Join(tempDir, "non-existent-path"),
		PreserveDir: true,
	}

	// Create the blob.
	dirBlob, err = dir.GetV1DirBlob(ctx, dirSpec)
	// Expect no error here, because the pipe is not processed yet.
	require.NoError(t, err)
	require.NotNil(t, dirBlob)

	// Try to read the data. Expect error propagation from the pipe packaging the tar.
	// Getting the reader should fail. The error: "... non-existent-path: no such file or directory".
	reader, err := dirBlob.ReadCloser()
	assert.Error(t, err)
	assert.Nil(t, reader)
}

// extractFileFromTar extracts a specific file from a tar archive and returns its content
func extractFileFromTar(tarData []byte, fileName string) ([]byte, error) {
	// Create a reader from the byte data.
	reader := bytes.NewReader(tarData)

	// Create a tar reader.
	tr := tar.NewReader(reader)

	// Normalize the file name for comparison.
	normalizedFileName := filepath.Clean(fileName)

	// Iterate through the files in the tar archive.
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break // End of archive.
		}
		if err != nil {
			return nil, fmt.Errorf("error reading tar header: %w", err)
		}

		// Normalize the header name for comparison.
		normalizedHeaderName := filepath.Clean(header.Name)

		// Check if this is the file we're looking for.
		if normalizedHeaderName == normalizedFileName {
			// Make sure it's a regular file.
			if header.Typeflag != tar.TypeReg {
				return nil, fmt.Errorf("'%s' is not a regular file (type: %c)", fileName, header.Typeflag)
			}

			// Read the file content.
			content, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("error reading file content: %w", err)
			}

			return content, nil
		}
	}

	// File not found.
	return nil, fmt.Errorf("file '%s' not found in tar archive", fileName)
}
