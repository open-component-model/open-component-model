package transformer_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/oci/transformer"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
)

// mockBlobWithoutMediaType is a simple blob that does not implement MediaTypeAware
type mockBlobWithoutMediaType struct{}

func (m *mockBlobWithoutMediaType) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader([]byte("test"))), nil
}

// mockBlobMediaTypeNotKnown wraps a blob but returns false for MediaType().known
type mockBlobMediaTypeNotKnown struct {
	blob blob.ReadOnlyBlob
}

func (m *mockBlobMediaTypeNotKnown) ReadCloser() (io.ReadCloser, error) {
	return m.blob.ReadCloser()
}

func (m *mockBlobMediaTypeNotKnown) MediaType() (string, bool) {
	return "", false
}

func Test_DetermineOutputPath_EmptyPath(t *testing.T) {
	tests := []struct {
		name              string
		setupBlob         func() blob.ReadOnlyBlob
		prefix            string
		expectedExtension string
	}{
		{
			name: "known media type",
			setupBlob: func() blob.ReadOnlyBlob {
				b := inmemory.New(bytes.NewReader([]byte("test")))
				b.SetMediaType("application/vnd.ocm.software.oci.layout.v1+tar+gzip")
				return b
			},
			prefix:            "test-prefix",
			expectedExtension: ".tar.gz",
		},
		{
			name: "unknown media type",
			setupBlob: func() blob.ReadOnlyBlob {
				b := inmemory.New(bytes.NewReader([]byte("test")))
				b.SetMediaType("application/unknown")
				return b
			},
			prefix:            "test-prefix",
			expectedExtension: ".bin",
		},
		{
			name: "without MediaTypeAware",
			setupBlob: func() blob.ReadOnlyBlob {
				return &mockBlobWithoutMediaType{}
			},
			prefix:            "test-prefix",
			expectedExtension: ".bin",
		},
		{
			name: "MediaType not known",
			setupBlob: func() blob.ReadOnlyBlob {
				return &mockBlobMediaTypeNotKnown{
					blob: inmemory.New(bytes.NewReader([]byte("test"))),
				}
			},
			prefix:            "test-prefix",
			expectedExtension: ".bin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputPath, err := transformer.DetermineOutputPath("", tt.prefix, tt.setupBlob())

			require.NoError(t, err)
			assert.NotEmpty(t, outputPath)
			assert.True(t, strings.HasSuffix(outputPath, tt.expectedExtension))
			assert.Contains(t, filepath.Base(outputPath), tt.prefix)

			// Verify file exists and clean up
			_, err = os.Stat(outputPath)
			assert.NoError(t, err)
			_ = os.Remove(outputPath)
		})
	}
}

func Test_DetermineOutputPath_ProvidedPath(t *testing.T) {
	tests := []struct {
		name     string
		setupDir func(*testing.T) (string, string) // returns tempDir, outputPath
	}{
		{
			name: "existing directory",
			setupDir: func(t *testing.T) (string, string) {
				tempDir := t.TempDir()
				return tempDir, filepath.Join(tempDir, "output.tar.gz")
			},
		},
		{
			name: "nested directories",
			setupDir: func(t *testing.T) (string, string) {
				tempDir := t.TempDir()
				return tempDir, filepath.Join(tempDir, "level1", "level2", "output.tar.gz")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir, outputPath := tt.setupDir(t)
			testBlob := inmemory.New(bytes.NewReader([]byte("test")))

			result, err := transformer.DetermineOutputPath(outputPath, "ignored", testBlob)

			require.NoError(t, err)
			assert.Equal(t, outputPath, result)

			// Verify directory exists
			info, err := os.Stat(filepath.Dir(result))
			require.NoError(t, err)
			assert.True(t, info.IsDir())

			// Verify within temp directory
			assert.True(t, strings.HasPrefix(result, tempDir))
		})
	}
}
