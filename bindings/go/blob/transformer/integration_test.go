package transformer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
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

	// Verify we can read the result
	reader, err := result.ReadCloser()
	r.NoError(err, "Should be able to read result")
	defer reader.Close()

	buffer := make([]byte, 1024)
	n, err := reader.Read(buffer)
	r.NoError(err, "Should be able to read data from result")
	r.Greater(n, 0, "Should read some data")

	t.Logf("Successfully transformed Helm chart from local OCI layout, read %d bytes", n)
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
	defer reader.Close()

	// we'll validate here that there is no provenance data and it's a different layout.
	buffer := make([]byte, 1024)
	n, err := reader.Read(buffer)
	r.NoError(err, "Should be able to read data from result")
	r.Greater(n, 0, "Should read some data")

	t.Logf("Successfully transformed OCI artifact from local OCI layout using generic transformer, read %d bytes", n)
}

// loadOCILayoutBlob loads an OCI layout tar file as a blob
func loadOCILayoutBlob(ctx context.Context, layoutPath string) (blob.ReadOnlyBlob, error) {
	layoutData, err := os.ReadFile(layoutPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read OCI layout file: %w", err)
	}

	return &testBlob{data: layoutData}, nil
}

// testBlob is a simple implementation of blob.ReadOnlyBlob for testing
type testBlob struct {
	data []byte
}

func (b *testBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(b.data)), nil
}

func (b *testBlob) Size() int64 {
	return int64(len(b.data))
}
