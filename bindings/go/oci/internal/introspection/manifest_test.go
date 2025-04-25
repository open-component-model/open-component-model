package introspection_test

import (
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/oci/internal/introspection"
)

func TestIsManifest(t *testing.T) {
	tests := []struct {
		name      string
		mediaType string
		expected  bool
	}{
		{
			name:      "oci image manifest",
			mediaType: ociImageSpecV1.MediaTypeImageManifest,
			expected:  true,
		},
		{
			name:      "oci image index",
			mediaType: ociImageSpecV1.MediaTypeImageIndex,
			expected:  true,
		},
		{
			name:      "non-manifest media type",
			mediaType: "application/octet-stream",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desc := ociImageSpecV1.Descriptor{
				MediaType: tt.mediaType,
			}
			result := introspection.IsOCICompliantManifest(desc)
			assert.Equal(t, tt.expected, result)
		})
	}
}
