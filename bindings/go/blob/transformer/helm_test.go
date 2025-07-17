package transformer

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
)

func TestHelmTransformer_TransformBlob(t *testing.T) {
	tests := []struct {
		name        string
		setupBlob   func(t *testing.T) *inmemory.Blob
		expectError bool
	}{
		{
			name: "valid helm chart OCI artifact",
			setupBlob: func(t *testing.T) *inmemory.Blob {
				return createHelmChartOCIBlob(t)
			},
			expectError: false,
		},
		{
			name: "invalid blob data",
			setupBlob: func(t *testing.T) *inmemory.Blob {
				return inmemory.New(bytes.NewReader([]byte("not a valid tar")))
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transformer := NewHelmTransformer()
			inputBlob := tt.setupBlob(t)

			result, err := transformer.TransformBlob(context.TODO(), inputBlob, nil)

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

func TestHelmTransformer_getHelmFilename(t *testing.T) {
	transformer := NewHelmTransformer()

	tests := []struct {
		mediaType string
		expected  string
	}{
		{MediaTypeHelmChart, "chart.tar.gz"},
		{MediaTypeHelmProvenance, "chart.prov"},
		{MediaTypeHelmConfig, "config.json"},
		{"application/tar", "helm-content.tar"},
		{"application/tar+gzip", "helm-content.tar.gz"},
		{"application/json", "helm-config.json"},
		{"application/vnd.example.prov", "helm-provenance.prov"},
		{"application/octet-stream", "helm-layer.bin"},
	}

	for _, tt := range tests {
		t.Run(tt.mediaType, func(t *testing.T) {
			result := transformer.getHelmFilename(tt.mediaType)
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

func TestHelmTransformerDispatch(t *testing.T) {
	helmBlob := &MockMediaTypeAwareBlob{
		Blob:      createHelmChartOCIBlob(t),
		mediaType: MediaTypeHelmChart,
	}

	result, err := TransformBlob(t.Context(), helmBlob, nil)

	assert.NoError(t, err)
	assert.NotNil(t, result)

	if mediaTypeAware, ok := result.(blob.MediaTypeAware); ok {
		mediaType, known := mediaTypeAware.MediaType()
		assert.True(t, known)
		assert.Equal(t, "application/tar", mediaType)
	}
}

func TestHelmTransformerIntegration(t *testing.T) {
	inputBlob := &MockMediaTypeAwareBlob{
		Blob:      createHelmChartOCIBlob(t),
		mediaType: MediaTypeHelmChart,
	}

	result, err := TransformBlob(t.Context(), inputBlob, nil)

	assert.NoError(t, err)
	assert.NotNil(t, result)

	if mediaTypeAware, ok := result.(blob.MediaTypeAware); ok {
		mediaType, known := mediaTypeAware.MediaType()
		assert.True(t, known)
		assert.Equal(t, "application/tar", mediaType)
	}
}
