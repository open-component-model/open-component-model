package component_test

import (
	"context"
	"fmt"
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/oci/internal/lister"
	"ocm.software/open-component-model/bindings/go/oci/internal/lister/component"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	"ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
)

type mockStore struct {
	resolveFunc func(ctx context.Context, ref string) (ociImageSpecV1.Descriptor, error)
}

func (m *mockStore) Resolve(ctx context.Context, ref string) (ociImageSpecV1.Descriptor, error) {
	return m.resolveFunc(ctx, ref)
}

func TestReferrerAnnotationVersionResolver(t *testing.T) {
	tests := []struct {
		name          string
		component     string
		descriptor    ociImageSpecV1.Descriptor
		expected      string
		expectedError error
	}{
		{
			name:      "valid component version",
			component: "component-descriptors/test-component",
			descriptor: ociImageSpecV1.Descriptor{
				Annotations: map[string]string{
					annotations.OCMComponentVersion: "component-descriptors/test-component:v1.0.0",
				},
			},
			expected: "v1.0.0",
		},
		{
			name:      "missing annotations",
			component: "test-component",
			descriptor: ociImageSpecV1.Descriptor{
				Annotations: nil,
			},
			expectedError: lister.ErrSkip,
		},
		{
			name:      "missing component version annotation",
			component: "test-component",
			descriptor: ociImageSpecV1.Descriptor{
				Annotations: map[string]string{
					"other-annotation": "value",
				},
			},
			expectedError: lister.ErrSkip,
		},
		{
			name:      "invalid annotation format",
			component: "test-component",
			descriptor: ociImageSpecV1.Descriptor{
				Annotations: map[string]string{
					annotations.OCMComponentVersion: "invalid-format",
				},
			},
			expectedError: fmt.Errorf("failed to parse component version annotation: %q is not considered a valid %q annotation because of a bad prefix, expected %q", "invalid-format", annotations.OCMComponentVersion, annotations.DefaultComponentDescriptorPath+"/"),
		},
		{
			name:      "component name mismatch",
			component: "test-component",
			descriptor: ociImageSpecV1.Descriptor{
				Annotations: map[string]string{
					annotations.OCMComponentVersion: "component-descriptors/other-component:v1.0.0",
				},
			},
			expectedError: fmt.Errorf("component %q from annotation does not match %q: %w", "other-component", "test-component", lister.ErrSkip),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := component.ReferrerAnnotationVersionResolver(tt.component)
			result, err := resolver(t.Context(), tt.descriptor)

			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestReferenceTagVersionResolver(t *testing.T) {
	tests := []struct {
		name          string
		ref           string
		tag           string
		store         content.Resolver
		expected      string
		expectedError error
	}{
		{
			name: "valid legacy manifest",
			ref:  "example.com/repo",
			tag:  "v1.0.0",
			store: &mockStore{
				resolveFunc: func(ctx context.Context, ref string) (ociImageSpecV1.Descriptor, error) {
					return ociImageSpecV1.Descriptor{
						MediaType: ociImageSpecV1.MediaTypeImageManifest,
					}, nil
				},
			},
			expected: "v1.0.0",
		},
		{
			name: "valid current manifest",
			ref:  "example.com/repo",
			tag:  "v1.0.0",
			store: &mockStore{
				resolveFunc: func(ctx context.Context, ref string) (ociImageSpecV1.Descriptor, error) {
					return ociImageSpecV1.Descriptor{
						MediaType:    ociImageSpecV1.MediaTypeImageManifest,
						ArtifactType: descriptor.MediaTypeComponentDescriptorV2,
					}, nil
				},
			},
			expected: "v1.0.0",
		},
		{
			name: "valid current index",
			ref:  "example.com/repo",
			tag:  "v1.0.0",
			store: &mockStore{
				resolveFunc: func(ctx context.Context, ref string) (ociImageSpecV1.Descriptor, error) {
					return ociImageSpecV1.Descriptor{
						MediaType:    ociImageSpecV1.MediaTypeImageIndex,
						ArtifactType: descriptor.MediaTypeComponentDescriptorV2,
					}, nil
				},
			},
			expected: "v1.0.0",
		},
		{
			name: "invalid media type",
			ref:  "example.com/repo",
			tag:  "v1.0.0",
			store: &mockStore{
				resolveFunc: func(ctx context.Context, ref string) (ociImageSpecV1.Descriptor, error) {
					return ociImageSpecV1.Descriptor{
						MediaType:    "invalid/type",
						ArtifactType: descriptor.MediaTypeComponentDescriptorV2,
					}, nil
				},
			},
			expected:      "v1.0.0",
			expectedError: lister.ErrSkip,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := component.ReferenceTagVersionResolver(tt.store)

			result, err := resolver(t.Context(), tt.tag)

			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError.Error())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}
