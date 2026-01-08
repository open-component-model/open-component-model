package component_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/oci/internal/lister"
	"ocm.software/open-component-model/bindings/go/oci/internal/lister/component"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	componentConfig "ocm.software/open-component-model/bindings/go/oci/spec/config/component"
)

type mockStore struct {
	resolveFunc func(ctx context.Context, ref string) (ociImageSpecV1.Descriptor, error)
	fetchFunc   func(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error)
}

func (m *mockStore) Resolve(ctx context.Context, ref string) (ociImageSpecV1.Descriptor, error) {
	return m.resolveFunc(ctx, ref)
}

func (m *mockStore) Fetch(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	return m.fetchFunc(ctx, desc)
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
			component: "ocm.software/test-component",
			descriptor: ociImageSpecV1.Descriptor{
				Annotations: nil,
			},
			expectedError: lister.ErrSkip,
		},
		{
			name:      "missing component version annotation",
			component: "ocm.software/test-component",
			descriptor: ociImageSpecV1.Descriptor{
				Annotations: map[string]string{
					"other-annotation": "value",
				},
			},
			expectedError: lister.ErrSkip,
		},
		{
			name:      "invalid annotation format",
			component: "ocm.software/test-component",
			descriptor: ociImageSpecV1.Descriptor{
				Annotations: map[string]string{
					annotations.OCMComponentVersion: "invalid-format",
				},
			},
			expectedError: fmt.Errorf("failed to parse component version annotation: %q is not considered a valid %q annotation, not exactly 2 parts: [%[1]q]", "invalid-format", annotations.OCMComponentVersion),
		},
		{
			name:      "component name mismatch",
			component: "ocm.software/test-component",
			descriptor: ociImageSpecV1.Descriptor{
				Annotations: map[string]string{
					annotations.OCMComponentVersion: "component-descriptors/other-component:v1.0.0",
				},
			},
			expectedError: fmt.Errorf("component %q from annotation does not match %q: %w", "other-component", "ocm.software/test-component", lister.ErrSkip),
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
		store         *mockStore
		expected      string
		expectedError error
	}{
		{
			name: "valid manifest",
			ref:  "example.com/repo",
			tag:  "v1.0.0",
			store: &mockStore{
				resolveFunc: func(ctx context.Context, ref string) (ociImageSpecV1.Descriptor, error) {
					return ociImageSpecV1.Descriptor{
						MediaType: ociImageSpecV1.MediaTypeImageManifest,
					}, nil
				},
				fetchFunc: func(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
					data, err := json.Marshal(&ociImageSpecV1.Manifest{
						MediaType: ociImageSpecV1.MediaTypeImageManifest,
						Annotations: map[string]string{
							annotations.OCMComponentVersion: annotations.NewComponentVersionAnnotation("example.com/repo", "v1.0.0"),
						},
					})
					if err != nil {
						return nil, fmt.Errorf("failed to marshal manifest: %w", err)
					}
					return io.NopCloser(bytes.NewReader(data)), nil
				},
			},
			expected: "v1.0.0",
		},
		{
			name: "valid index manifest",
			ref:  "example.com/repo",
			tag:  "v1.0.0",
			store: &mockStore{
				resolveFunc: func(ctx context.Context, ref string) (ociImageSpecV1.Descriptor, error) {
					return ociImageSpecV1.Descriptor{
						MediaType: ociImageSpecV1.MediaTypeImageIndex,
					}, nil
				},
				fetchFunc: func(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
					data, err := json.Marshal(&ociImageSpecV1.Index{
						MediaType: ociImageSpecV1.MediaTypeImageIndex,
						Annotations: map[string]string{
							annotations.OCMComponentVersion: annotations.NewComponentVersionAnnotation("example.com/repo", "v1.0.0"),
						},
					})
					if err != nil {
						return nil, fmt.Errorf("failed to marshal manifest: %w", err)
					}
					return io.NopCloser(bytes.NewReader(data)), nil
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
						MediaType: "invalid/type",
					}, nil
				},
			},
			expected:      "v1.0.0",
			expectedError: lister.ErrSkip,
		},
		{
			name: "missing annotation",
			ref:  "example.com/repo",
			tag:  "v1.0.0",
			store: &mockStore{
				resolveFunc: func(ctx context.Context, ref string) (ociImageSpecV1.Descriptor, error) {
					return ociImageSpecV1.Descriptor{
						MediaType: ociImageSpecV1.MediaTypeImageIndex,
					}, nil
				},
				fetchFunc: func(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
					data, err := json.Marshal(&ociImageSpecV1.Index{
						MediaType: ociImageSpecV1.MediaTypeImageIndex,
					})
					if err != nil {
						return nil, fmt.Errorf("failed to marshal manifest: %w", err)
					}
					return io.NopCloser(bytes.NewReader(data)), nil
				},
			},
			expected:      "v1.0.0",
			expectedError: lister.ErrSkip,
		},
		{
			name: "old component version without annotation (pre-2024)",
			ref:  "example.com/repo",
			tag:  "v1.0.0",
			store: &mockStore{
				resolveFunc: func(ctx context.Context, ref string) (ociImageSpecV1.Descriptor, error) {
					return ociImageSpecV1.Descriptor{
						MediaType: ociImageSpecV1.MediaTypeImageManifest,
					}, nil
				},
				fetchFunc: func(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
					data, err := json.Marshal(&ociImageSpecV1.Manifest{
						MediaType: ociImageSpecV1.MediaTypeImageManifest,
						Config: ociImageSpecV1.Descriptor{
							MediaType: componentConfig.MediaType,
						},
					})
					if err != nil {
						return nil, fmt.Errorf("failed to marshal manifest: %w", err)
					}
					return io.NopCloser(bytes.NewReader(data)), nil
				},
			},
			expected: "v1.0.0",
		},
		{
			name: "old component version with annotation present (not fallback case)",
			ref:  "example.com/repo",
			tag:  "v1.0.0",
			store: &mockStore{
				resolveFunc: func(ctx context.Context, ref string) (ociImageSpecV1.Descriptor, error) {
					return ociImageSpecV1.Descriptor{
						MediaType: ociImageSpecV1.MediaTypeImageManifest,
					}, nil
				},
				fetchFunc: func(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
					data, err := json.Marshal(&ociImageSpecV1.Manifest{
						MediaType: ociImageSpecV1.MediaTypeImageManifest,
						Config: ociImageSpecV1.Descriptor{
							MediaType: componentConfig.MediaType,
						},
						Annotations: map[string]string{
							annotations.OCMComponentVersion: annotations.NewComponentVersionAnnotation("example.com/repo", "v1.0.0"),
						},
					})
					if err != nil {
						return nil, fmt.Errorf("failed to marshal manifest: %w", err)
					}
					return io.NopCloser(bytes.NewReader(data)), nil
				},
			},
			expected: "v1.0.0",
		},
		{
			name: "manifest without annotation and wrong config media type",
			ref:  "example.com/repo",
			tag:  "v1.0.0",
			store: &mockStore{
				resolveFunc: func(ctx context.Context, ref string) (ociImageSpecV1.Descriptor, error) {
					return ociImageSpecV1.Descriptor{
						MediaType: ociImageSpecV1.MediaTypeImageManifest,
					}, nil
				},
				fetchFunc: func(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
					data, err := json.Marshal(&ociImageSpecV1.Manifest{
						MediaType: ociImageSpecV1.MediaTypeImageManifest,
						Config: ociImageSpecV1.Descriptor{
							MediaType: "application/vnd.oci.image.config.v1+json",
						},
					})
					if err != nil {
						return nil, fmt.Errorf("failed to marshal manifest: %w", err)
					}
					return io.NopCloser(bytes.NewReader(data)), nil
				},
			},
			expected:      "v1.0.0",
			expectedError: lister.ErrSkip,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := component.ReferenceTagVersionResolver(tt.ref, tt.store)

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
