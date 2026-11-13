package annotations

import (
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestGetArtifactOCILayerAnnotations(t *testing.T) {
	tests := []struct {
		name          string
		descriptor    *ociImageSpecV1.Descriptor
		expected      []ArtifactOCIAnnotation
		expectedError error
	}{
		{
			name: "valid artifact annotation",
			descriptor: &ociImageSpecV1.Descriptor{
				Annotations: map[string]string{
					ArtifactAnnotationKey: `[{"identity":{"name":"test","version":"1.0.0"},"kind":"source"}]`,
				},
			},
			expected: []ArtifactOCIAnnotation{
				{
					Identity: runtime.Identity{
						"name":    "test",
						"version": "1.0.0",
					},
					Kind: ArtifactKindSource,
				},
			},
			expectedError: nil,
		},
		{
			name: "no artifact annotation",
			descriptor: &ociImageSpecV1.Descriptor{
				Annotations: map[string]string{},
			},
			expected:      nil,
			expectedError: ErrArtifactOCILayerAnnotationDoesNotExist,
		},
		{
			name: "invalid json in annotation",
			descriptor: &ociImageSpecV1.Descriptor{
				Annotations: map[string]string{
					ArtifactAnnotationKey: `invalid json`,
				},
			},
			expected:      nil,
			expectedError: nil, // json.Unmarshal error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GetArtifactOCILayerAnnotations(tt.descriptor)
			if tt.expectedError != nil {
				assert.ErrorIs(t, err, tt.expectedError)
			} else {
				if err != nil {
					assert.Error(t, err)
				} else {
					assert.Equal(t, tt.expected, result)
				}
			}
		})
	}
}

func TestAddToDescriptor(t *testing.T) {
	tests := []struct {
		name          string
		annotation    ArtifactOCIAnnotation
		descriptor    *ociImageSpecV1.Descriptor
		expected      string
		expectedError error
	}{
		{
			name: "add to empty descriptor",
			annotation: ArtifactOCIAnnotation{
				Identity: runtime.Identity{
					"name":    "test",
					"version": "1.0.0",
				},
				Kind: ArtifactKindSource,
			},
			descriptor: &ociImageSpecV1.Descriptor{
				Annotations: map[string]string{},
			},
			expected:      `[{"identity":{"name":"test","version":"1.0.0"},"kind":"source"}]`,
			expectedError: nil,
		},
		{
			name: "add to existing annotations",
			annotation: ArtifactOCIAnnotation{
				Identity: runtime.Identity{
					"name":    "test2",
					"version": "2.0.0",
				},
				Kind: ArtifactKindResource,
			},
			descriptor: &ociImageSpecV1.Descriptor{
				Annotations: map[string]string{
					ArtifactAnnotationKey: `[{"identity":{"name":"test","version":"1.0.0"},"kind":"source"}]`,
				},
			},
			expected:      `[{"identity":{"name":"test","version":"1.0.0"},"kind":"source"},{"identity":{"name":"test2","version":"2.0.0"},"kind":"resource"}]`,
			expectedError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.annotation.AddToDescriptor(tt.descriptor)
			if tt.expectedError != nil {
				assert.ErrorIs(t, err, tt.expectedError)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, tt.descriptor.Annotations[ArtifactAnnotationKey])
			}
		})
	}
}

// The annotation is stored in the manifest and therefore part of its digest, so the bytes
// must stay what encoding/json v1 produced, which escaped <, > and & as well as the line
// terminators U+2028 and U+2029.
func TestAddToDescriptor_EscapesLikeV1(t *testing.T) {
	desc := &ociImageSpecV1.Descriptor{}
	annotation := ArtifactOCIAnnotation{
		Identity: runtime.Identity{"name": "a<b>&c\u2028d\u2029e"},
		Kind:     ArtifactKindResource,
	}
	require.NoError(t, annotation.AddToDescriptor(desc))

	// The exact bytes are the point here, not JSON equality.
	assert.Equal(t, //nolint:testifylint // encoded-compare: the escaping is what is under test
		`[{"identity":{"name":"a\u003cb\u003e\u0026c\u2028d\u2029e"},"kind":"resource"}]`,
		desc.Annotations[ArtifactAnnotationKey])
}

func TestIsArtifactForResource(t *testing.T) {
	tests := []struct {
		name       string
		descriptor ociImageSpecV1.Descriptor
		identity   runtime.Identity
		kind       ArtifactKind
		expected   bool
	}{
		{
			name: "matching artifact",
			descriptor: ociImageSpecV1.Descriptor{
				Annotations: map[string]string{
					ArtifactAnnotationKey: `[{"identity":{"name":"test","version":"1.0.0"},"kind":"source"}]`,
				},
			},
			identity: runtime.Identity{
				"name":    "test",
				"version": "1.0.0",
			},
			kind:     ArtifactKindSource,
			expected: true,
		},
		{
			name: "non-matching kind",
			descriptor: ociImageSpecV1.Descriptor{
				Annotations: map[string]string{
					ArtifactAnnotationKey: `[{"identity":{"name":"test","version":"1.0.0"},"kind":"source"}]`,
				},
			},
			identity: runtime.Identity{
				"name":    "test",
				"version": "1.0.0",
			},
			kind:     ArtifactKindResource,
			expected: false,
		},
		{
			name: "non-matching identity",
			descriptor: ociImageSpecV1.Descriptor{
				Annotations: map[string]string{
					ArtifactAnnotationKey: `[{"identity":{"name":"test","version":"1.0.0"},"kind":"source"}]`,
				},
			},
			identity: runtime.Identity{
				"name":    "different",
				"version": "1.0.0",
			},
			kind:     ArtifactKindSource,
			expected: false,
		},
		{
			name: "no annotations",
			descriptor: ociImageSpecV1.Descriptor{
				Annotations: map[string]string{},
			},
			identity: runtime.Identity{
				"name":    "test",
				"version": "1.0.0",
			},
			kind:     ArtifactKindSource,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsArtifactForResource(tt.descriptor, tt.identity, tt.kind)
			assert.Equal(t, tt.expected, result)
		})
	}
}
