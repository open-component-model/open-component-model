package transformer

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
)

func TestTransformer_TransformBlob(t *testing.T) {
	tests := []struct {
		name        string
		setupBlob   func(t *testing.T) blob.ReadOnlyBlob
		expectError bool
	}{
		{
			name: "valid OCI artifact",
			setupBlob: func(t *testing.T) blob.ReadOnlyBlob {
				return createOCILayoutBlob(t)
			},
			expectError: false,
		},
		{
			name: "invalid blob data",
			setupBlob: func(t *testing.T) blob.ReadOnlyBlob {
				return inmemory.New(bytes.NewReader([]byte("not a valid tar")))
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transformer := New()
			inputBlob := tt.setupBlob(t)

			result, err := transformer.TransformBlob(context.Background(), inputBlob, nil)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)

				if mediaTypeAware, ok := result.(blob.MediaTypeAware); ok {
					mediaType, known := mediaTypeAware.MediaType()
					assert.True(t, known)
					assert.Equal(t, "application/tar", mediaType)
				}
			}
		})
	}
}

func TestTransformer_getFilename(t *testing.T) {
	transformer := New()

	tests := []struct {
		mediaType string
		expected  string
	}{
		{MediaTypeHelmChart, "chart.tar.gz"},
		{MediaTypeHelmProvenance, "chart.prov"},
		{MediaTypeHelmConfig, "config.json"},
		{"application/tar", "layer.tar"},
		{"application/tar+gzip", "layer.tar.gz"},
		{"application/json", "layer.json"},
		{"application/octet-stream", "layer.bin"},
	}

	for _, tt := range tests {
		t.Run(tt.mediaType, func(t *testing.T) {
			result := transformer.GetFilename(tt.mediaType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsHelmMediaType(t *testing.T) {
	tests := []struct {
		mediaType string
		expected  bool
	}{
		{MediaTypeHelmChart, true},
		{MediaTypeHelmProvenance, true},
		{MediaTypeHelmConfig, true},
		{"application/tar", false},
		{"application/json", false},
		{"application/octet-stream", false},
	}

	for _, tt := range tests {
		t.Run(tt.mediaType, func(t *testing.T) {
			result := IsHelmMediaType(tt.mediaType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTransformerIntegration(t *testing.T) {
	ctx := context.Background()
	r := require.New(t)

	ociLayoutBlob, err := loadOCILayoutBlob(ctx, "oci-layout.tar.gz")
	r.NoError(err)
	r.NotNil(ociLayoutBlob, "OCI layout blob should not be nil")

	transformer := New()
	result, err := transformer.TransformBlob(ctx, ociLayoutBlob, nil)

	r.NoError(err, "Transformation should succeed")
	r.NotNil(result, "Result should not be nil")

	if mediaTypeAware, ok := result.(blob.MediaTypeAware); ok {
		mediaType, known := mediaTypeAware.MediaType()
		r.True(known, "Media type should be known")
		r.Equal("application/tar", mediaType, "Result should be tar format")
	}

	reader, err := result.ReadCloser()
	r.NoError(err, "Should be able to read result")

	expectedFiles := []string{"chart.prov", "chart.tar.gz"}
	validateTarContents(t, reader, expectedFiles)

	t.Logf("Successfully transformed and validated OCI artifact")
}

// createOCILayoutBlob creates a test blob with OCI layout data
func createOCILayoutBlob(t *testing.T) blob.ReadOnlyBlob {
	data, err := os.ReadFile(filepath.Join("testdata", "oci-layout.tar.gz"))
	require.NoError(t, err, "Failed to read test data")
	return &testBlob{data: data}
}

// loadOCILayoutBlob loads an OCI layout tar file as a blob.
func loadOCILayoutBlob(ctx context.Context, layoutPath string) (blob.ReadOnlyBlob, error) {
	layoutData, err := os.ReadFile(filepath.Join("testdata", layoutPath))
	if err != nil {
		return nil, fmt.Errorf("failed to read OCI layout file: %w", err)
	}

	return &testBlob{data: layoutData}, nil
}

// testBlob is a simple implementation of blob.ReadOnlyBlob for testing.
type testBlob struct {
	data []byte
}

func (b *testBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(b.data)), nil
}

func (b *testBlob) Size() int64 {
	return int64(len(b.data))
}

// validateTarContents validates that specific files are present in the tar.
func validateTarContents(t *testing.T, reader io.ReadCloser, expectedFiles []string) {
	defer reader.Close()

	data, err := io.ReadAll(reader)
	require.NoError(t, err, "Should be able to read all data from tar")

	tarReader := tar.NewReader(bytes.NewReader(data))
	foundFiles := make(map[string]bool)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err, "Should be able to read tar header")

		filename := header.Name
		if strings.Contains(filename, "/") {
			parts := strings.Split(filename, "/")
			filename = parts[len(parts)-1]
		}

		if filename != "" {
			foundFiles[filename] = true
			t.Logf("Found file in tar: %s (original path: %s)", filename, header.Name)
		}
	}

	for _, expectedFile := range expectedFiles {
		require.True(t, foundFiles[expectedFile], "Expected file %s should be present in tar", expectedFile)
	}

	t.Logf("Successfully validated tar contains all expected files: %v", expectedFiles)
}
