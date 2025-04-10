package blob_test

import (
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ociblob "ocm.software/open-component-model/bindings/go/oci/blob"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/digest/v1"
)

// mockBlob implements blob.ReadOnlyBlob for testing purposes
type mockBlob struct {
	blob.ReadOnlyBlob
}

func TestNewResourceBlob(t *testing.T) {
	resource := &descriptor.Resource{
		Digest: &descriptor.Digest{
			HashAlgorithm: v1.HashAlgorithmSHA256,
			Value:         "1234567890abcdef",
		},
		Size: 100,
	}
	mock := &mockBlob{}
	mediaType := "application/octet-stream"

	rb, err := ociblob.NewResourceBlobWithMediaType(resource, mock, mediaType)
	require.NoError(t, err)
	assert.NotNil(t, rb)
	assert.Equal(t, resource, rb.Resource)
	got, ok := rb.MediaType()
	assert.True(t, ok)
	assert.Equal(t, mediaType, got)
}

func TestResourceBlob_MediaType(t *testing.T) {
	resource := &descriptor.Resource{}
	mock := &mockBlob{}
	mediaType := "application/octet-stream"

	rb, err := ociblob.NewResourceBlobWithMediaType(resource, mock, mediaType)
	require.NoError(t, err)
	mt, ok := rb.MediaType()
	assert.True(t, ok)
	assert.Equal(t, mediaType, mt)
}

func TestResourceBlob_Digest(t *testing.T) {
	tests := []struct {
		name           string
		resource       *descriptor.Resource
		expectedDigest string
		expectedOK     bool
	}{
		{
			name: "valid sha256 digest",
			resource: &descriptor.Resource{
				Digest: &descriptor.Digest{
					HashAlgorithm: v1.HashAlgorithmSHA256,
					Value:         "1234567890abcdef",
				},
			},
			expectedDigest: "sha256:1234567890abcdef",
			expectedOK:     true,
		},
		{
			name: "empty hash algorithm defaults to canonical",
			resource: &descriptor.Resource{
				Digest: &descriptor.Digest{
					HashAlgorithm: v1.HashAlgorithmSHA256,
					Value:         "1234567890abcdef",
				},
			},
			expectedDigest: "sha256:1234567890abcdef",
			expectedOK:     true,
		},
		{
			name:           "nil digest",
			resource:       &descriptor.Resource{},
			expectedDigest: "",
			expectedOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBlob{}
			rb, err := ociblob.NewResourceBlobWithMediaType(tt.resource, mock, "application/octet-stream")
			assert.NoError(t, err)
			dig, ok := rb.Digest()
			assert.Equal(t, tt.expectedOK, ok)
			if tt.expectedOK {
				assert.Equal(t, tt.expectedDigest, dig)
			}
		})
	}
}

func TestResourceBlob_HasPrecalculatedDigest(t *testing.T) {
	tests := []struct {
		name     string
		resource *descriptor.Resource
		expected bool
	}{
		{
			name:     "nil digest",
			resource: &descriptor.Resource{},
			expected: false,
		},
		{
			name: "empty digest value",
			resource: &descriptor.Resource{
				Digest: &descriptor.Digest{
					HashAlgorithm: v1.HashAlgorithmSHA256,
					Value:         "",
				},
			},
			expected: false,
		},
		{
			name: "valid digest",
			resource: &descriptor.Resource{
				Digest: &descriptor.Digest{
					HashAlgorithm: v1.HashAlgorithmSHA256,
					Value:         "1234567890abcdef",
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBlob{}
			rb, err := ociblob.NewResourceBlobWithMediaType(tt.resource, mock, "application/octet-stream")
			require.NoError(t, err)
			assert.Equal(t, tt.expected, rb.HasPrecalculatedDigest())
		})
	}
}

func TestResourceBlob_SetPrecalculatedDigest(t *testing.T) {
	tests := []struct {
		name           string
		resource       *descriptor.Resource
		newDigest      string
		expectedDigest *descriptor.Digest
		expectPanic    bool
	}{
		{
			name: "existing digest in resource",
			resource: &descriptor.Resource{
				Digest: &descriptor.Digest{
					HashAlgorithm: v1.HashAlgorithmSHA256,
					Value:         "old-value",
				},
			},
			newDigest: digest.FromString("test").String(),
			expectedDigest: &descriptor.Digest{
				HashAlgorithm: v1.ReverseSHAMapping[digest.FromString("test").Algorithm()],
				Value:         digest.FromString("test").Encoded(),
			},
			expectPanic: false,
		},
		{
			name:        "invalid digest format",
			resource:    &descriptor.Resource{},
			newDigest:   "invalid-digest",
			expectPanic: true,
		},
		{
			name:        "nil digest in resource",
			resource:    &descriptor.Resource{},
			newDigest:   "sha256:1234567890abcdef",
			expectPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBlob{}
			rb, err := ociblob.NewResourceBlobWithMediaType(tt.resource, mock, "application/octet-stream")
			require.NoError(t, err)

			if tt.expectPanic {
				assert.Panics(t, func() {
					rb.SetPrecalculatedDigest(tt.newDigest)
				})
				return
			}

			rb.SetPrecalculatedDigest(tt.newDigest)
			assert.Equal(t, tt.expectedDigest, tt.resource.Digest)
		})
	}
}

func TestResourceBlob_Size(t *testing.T) {
	size := int64(100)
	resource := &descriptor.Resource{
		Size: size,
	}
	mock := &mockBlob{}

	rb, err := ociblob.NewResourceBlobWithMediaType(resource, mock, "application/octet-stream")
	require.NoError(t, err)
	assert.Equal(t, size, rb.Size())
}

func TestResourceBlob_HasPrecalculatedSize(t *testing.T) {
	tests := []struct {
		name     string
		resource *descriptor.Resource
		expected bool
	}{
		{
			name: "unknown size",
			resource: &descriptor.Resource{
				Size: blob.SizeUnknown,
			},
			expected: false,
		},
		{
			name: "valid size",
			resource: &descriptor.Resource{
				Size: 100,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBlob{}
			rb, err := ociblob.NewResourceBlobWithMediaType(tt.resource, mock, "application/octet-stream")
			require.NoError(t, err)
			assert.Equal(t, tt.expected, rb.HasPrecalculatedSize())
		})
	}
}

func TestResourceBlob_SetPrecalculatedSize(t *testing.T) {
	resource := &descriptor.Resource{}
	mock := &mockBlob{}

	rb, err := ociblob.NewResourceBlobWithMediaType(resource, mock, "application/octet-stream")
	require.NoError(t, err)
	newSize := int64(200)
	rb.SetPrecalculatedSize(newSize)
	assert.Equal(t, newSize, resource.Size)
}

func TestResourceBlob_OCIDescriptor(t *testing.T) {
	tests := []struct {
		name           string
		resource       *descriptor.Resource
		mediaType      string
		expectedDigest string
		expectedSize   int64
	}{
		{
			name: "valid descriptor",
			resource: &descriptor.Resource{
				Digest: &descriptor.Digest{
					HashAlgorithm: v1.HashAlgorithmSHA256,
					Value:         "1234567890abcdef",
				},
				Size: 100,
			},
			mediaType:      "application/octet-stream",
			expectedDigest: "sha256:1234567890abcdef",
			expectedSize:   100,
		},
		{
			name: "nil digest",
			resource: &descriptor.Resource{
				Size: 100,
			},
			mediaType:      "application/octet-stream",
			expectedDigest: "",
			expectedSize:   100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBlob{}
			rb, err := ociblob.NewResourceBlobWithMediaType(tt.resource, mock, tt.mediaType)
			require.NoError(t, err)
			desc := rb.OCIDescriptor()

			assert.Equal(t, tt.mediaType, desc.MediaType)
			assert.Equal(t, tt.expectedSize, desc.Size)
			if tt.expectedDigest != "" {
				assert.Equal(t, digest.Digest(tt.expectedDigest), desc.Digest)
			}
		})
	}
}

func TestResourceBlob_CompleteWorkflow(t *testing.T) {
	// Test a complete workflow using ResourceBlob
	resource := &descriptor.Resource{
		Digest: &descriptor.Digest{
			HashAlgorithm: v1.HashAlgorithmSHA256,
			Value:         "1234567890abcdef",
		},
		Size: 100,
	}
	mock := &mockBlob{}
	mediaType := "application/octet-stream"

	rb, err := ociblob.NewResourceBlobWithMediaType(resource, mock, mediaType)
	require.NoError(t, err)

	// Test all methods in sequence
	mt, ok := rb.MediaType()
	require.True(t, ok)
	assert.Equal(t, mediaType, mt)

	dig, ok := rb.Digest()
	require.True(t, ok)
	assert.Equal(t, "sha256:1234567890abcdef", dig)

	assert.Equal(t, resource.Size, rb.Size())
	assert.True(t, rb.HasPrecalculatedDigest())
	assert.True(t, rb.HasPrecalculatedSize())

	// Update values
	newDigest := digest.FromString("test")
	newSize := int64(200)
	rb.SetPrecalculatedDigest(newDigest.String())
	rb.SetPrecalculatedSize(newSize)

	// Verify updates
	dig, ok = rb.Digest()
	require.True(t, ok)
	assert.Equal(t, newDigest.String(), dig)
	assert.Equal(t, newSize, rb.Size())

	// Test OCI descriptor
	desc := rb.OCIDescriptor()
	assert.Equal(t, mediaType, desc.MediaType)
	assert.Equal(t, digest.Digest(newDigest), desc.Digest)
	assert.Equal(t, newSize, desc.Size)
}
