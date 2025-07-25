package transformer

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
)

// MockMediaTypeAwareBlob is a mock blob that implements MediaTypeAware.
type MockMediaTypeAwareBlob struct {
	*inmemory.Blob
	mediaType string
}

func (m *MockMediaTypeAwareBlob) MediaType() (string, bool) {
	return m.mediaType, true
}

func TestTransformBlob_HelmMediaTypes(t *testing.T) {
	tests := []struct {
		name          string
		mediaType     string
		expectNoError bool
	}{
		{
			name:          "helm chart media type",
			mediaType:     MediaTypeHelmChart,
			expectNoError: true,
		},
		{
			name:          "helm provenance media type",
			mediaType:     MediaTypeHelmProvenance,
			expectNoError: true,
		},
		{
			name:          "helm config media type",
			mediaType:     MediaTypeHelmConfig,
			expectNoError: true,
		},
		{
			name:          "unknown media type uses fallback",
			mediaType:     "application/unknown",
			expectNoError: true,
		},
		{
			name:          "default media type for blob without MediaTypeAware",
			mediaType:     "",
			expectNoError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var inputBlob blob.ReadOnlyBlob

			if tt.mediaType == "" {
				inputBlob = createHelmChartOCIBlob(t)
			} else {
				inputBlob = &MockMediaTypeAwareBlob{
					Blob:      createHelmChartOCIBlob(t),
					mediaType: tt.mediaType,
				}
			}

			result, err := TransformBlob(context.Background(), inputBlob, nil)

			if tt.expectNoError {
				assert.NoError(t, err)
				assert.NotNil(t, result)

				if mediaTypeAware, ok := result.(blob.MediaTypeAware); ok {
					mediaType, known := mediaTypeAware.MediaType()
					assert.True(t, known)
					assert.Equal(t, "application/tar", mediaType)
				}
			} else {
				assert.Error(t, err)
				assert.Nil(t, result)
			}
		})
	}
}

func TestTransformBlob_InvalidData(t *testing.T) {
	inputBlob := &MockMediaTypeAwareBlob{
		Blob:      inmemory.New(bytes.NewReader([]byte("invalid oci data"))),
		mediaType: "application/unknown",
	}

	result, err := TransformBlob(context.Background(), inputBlob, nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to read OCI layout")
}

func TestGetMediaType(t *testing.T) {
	tests := []struct {
		name      string
		setupBlob func() blob.ReadOnlyBlob
		expected  string
	}{
		{
			name: "blob with MediaTypeAware interface",
			setupBlob: func() blob.ReadOnlyBlob {
				return &MockMediaTypeAwareBlob{
					Blob:      inmemory.New(bytes.NewReader([]byte("test"))),
					mediaType: "application/test",
				}
			},
			expected: "application/test",
		},
		{
			name: "blob without MediaTypeAware interface",
			setupBlob: func() blob.ReadOnlyBlob {
				return inmemory.New(bytes.NewReader([]byte("test")))
			},
			expected: "application/octet-stream",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blob := tt.setupBlob()
			result := getMediaType(blob)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTransformBlob_RoutesToCorrectTransformer(t *testing.T) {
	helmBlob := &MockMediaTypeAwareBlob{
		Blob:      createHelmChartOCIBlob(t),
		mediaType: MediaTypeHelmChart,
	}

	unknownBlob := &MockMediaTypeAwareBlob{
		Blob:      createHelmChartOCIBlob(t),
		mediaType: "application/unknown",
	}

	helmResult, err := TransformBlob(context.Background(), helmBlob, nil)
	assert.NoError(t, err)
	assert.NotNil(t, helmResult)

	unknownResult, err := TransformBlob(context.Background(), unknownBlob, nil)
	assert.NoError(t, err)
	assert.NotNil(t, unknownResult)

	if helmMediaTypeAware, ok := helmResult.(blob.MediaTypeAware); ok {
		mediaType, known := helmMediaTypeAware.MediaType()
		assert.True(t, known)
		assert.Equal(t, "application/tar", mediaType)
	}

	if unknownMediaTypeAware, ok := unknownResult.(blob.MediaTypeAware); ok {
		mediaType, known := unknownMediaTypeAware.MediaType()
		assert.True(t, known)
		assert.Equal(t, "application/tar", mediaType)
	}
}
