package transformer

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
)

func TestIntegrationHelmChartTransformer(t *testing.T) {
	ctx := context.Background()
	r := require.New(t)

	ociLayoutBlob, err := loadOCILayoutBlob(ctx, "testdata/oci-layout.tar.gz")
	r.NoError(err)
	r.NotNil(ociLayoutBlob, "OCI layout blob should not be nil")

	transformer := NewHelmTransformer()
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

	t.Logf("Successfully transformed and validated Helm chart from local OCI layout")
}

func TestIntegrationOCIArtifactTransformer(t *testing.T) {
	ctx := context.Background()
	r := require.New(t)

	ociLayoutBlob, err := loadOCILayoutBlob(ctx, "testdata/oci-layout.tar.gz")
	r.NoError(err, "Failed to load OCI layout blob")
	r.NotNil(ociLayoutBlob, "OCI layout blob should not be nil")

	transformer := NewOCIArtifactTransformer()
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

	expectedFiles := []string{"layer.tar.gz", "layer.bin"}
	validateTarContents(t, reader, expectedFiles)

	t.Logf("Successfully transformed and validated OCI artifact from local OCI layout using generic transformer")
}

// loadOCILayoutBlob loads an OCI layout tar file as a blob.
func loadOCILayoutBlob(ctx context.Context, layoutPath string) (blob.ReadOnlyBlob, error) {
	layoutData, err := os.ReadFile(layoutPath)
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

// validateTarContents untars the downloaded oci artifact and verifies that certain files are present at given locations.
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
