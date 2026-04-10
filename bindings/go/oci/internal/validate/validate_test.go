package validate_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci/internal/validate"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	componentConfig "ocm.software/open-component-model/bindings/go/oci/spec/config/component"
)

type mockFetcher struct {
	fetchFunc func(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error)
}

func (m *mockFetcher) Fetch(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	return m.fetchFunc(ctx, desc)
}

func TestComponentVersionDescriptor(t *testing.T) {
	tests := []struct {
		name            string
		descriptor      ociImageSpecV1.Descriptor
		component       string
		tag             string
		fetcher         *mockFetcher
		expectedVersion string
		expectedError   error
	}{
		{
			name:      "valid manifest with annotation",
			component: "example.com/component",
			tag:       "v1.0.0",
			descriptor: ociImageSpecV1.Descriptor{
				MediaType: ociImageSpecV1.MediaTypeImageManifest,
			},
			fetcher: &mockFetcher{
				fetchFunc: func(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
					manifest := ociImageSpecV1.Manifest{
						Annotations: map[string]string{
							annotations.OCMComponentVersion: annotations.NewComponentVersionAnnotation("example.com/component", "v1.0.0"),
						},
					}
					data, _ := json.Marshal(manifest)
					return io.NopCloser(bytes.NewReader(data)), nil
				},
			},
			expectedVersion: "v1.0.0",
		},
		{
			name:      "valid index with annotation",
			component: "example.com/component",
			tag:       "v2.0.0",
			descriptor: ociImageSpecV1.Descriptor{
				MediaType: ociImageSpecV1.MediaTypeImageIndex,
			},
			fetcher: &mockFetcher{
				fetchFunc: func(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
					index := ociImageSpecV1.Index{
						Annotations: map[string]string{
							annotations.OCMComponentVersion: annotations.NewComponentVersionAnnotation("example.com/component", "v2.0.0"),
						},
					}
					data, _ := json.Marshal(index)
					return io.NopCloser(bytes.NewReader(data)), nil
				},
			},
			expectedVersion: "v2.0.0",
		},
		{
			name:      "component with descriptor path prefix",
			component: "component-descriptors/example.com/component",
			tag:       "v1.5.0",
			descriptor: ociImageSpecV1.Descriptor{
				MediaType: ociImageSpecV1.MediaTypeImageManifest,
			},
			fetcher: &mockFetcher{
				fetchFunc: func(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
					manifest := ociImageSpecV1.Manifest{
						Annotations: map[string]string{
							annotations.OCMComponentVersion: annotations.NewComponentVersionAnnotation("example.com/component", "v1.5.0"),
						},
					}
					data, _ := json.Marshal(manifest)
					return io.NopCloser(bytes.NewReader(data)), nil
				},
			},
			expectedVersion: "v1.5.0",
		},
		{
			name:      "legacy pre-2024 manifest without annotation",
			component: "example.com/component",
			tag:       "v0.9.0",
			descriptor: ociImageSpecV1.Descriptor{
				MediaType: ociImageSpecV1.MediaTypeImageManifest,
			},
			fetcher: &mockFetcher{
				fetchFunc: func(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
					manifest := ociImageSpecV1.Manifest{
						Config: ociImageSpecV1.Descriptor{
							MediaType: componentConfig.MediaType,
						},
					}
					data, _ := json.Marshal(manifest)
					return io.NopCloser(bytes.NewReader(data)), nil
				},
			},
			expectedVersion: "v0.9.0",
		},
		{
			name:      "unsupported media type",
			component: "example.com/component",
			tag:       "v1.0.0",
			descriptor: ociImageSpecV1.Descriptor{
				MediaType: "application/octet-stream",
			},
			expectedError: validate.ErrInvalidComponentVersion,
		},
		{
			name:      "manifest missing annotation and not legacy format",
			component: "example.com/component",
			tag:       "v1.0.0",
			descriptor: ociImageSpecV1.Descriptor{
				MediaType: ociImageSpecV1.MediaTypeImageManifest,
			},
			fetcher: &mockFetcher{
				fetchFunc: func(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
					manifest := ociImageSpecV1.Manifest{
						Config: ociImageSpecV1.Descriptor{
							MediaType: "application/vnd.oci.image.config.v1+json",
						},
					}
					data, _ := json.Marshal(manifest)
					return io.NopCloser(bytes.NewReader(data)), nil
				},
			},
			expectedError: validate.ErrInvalidComponentVersion,
		},
		{
			name:      "index missing annotation",
			component: "example.com/component",
			tag:       "v1.0.0",
			descriptor: ociImageSpecV1.Descriptor{
				MediaType: ociImageSpecV1.MediaTypeImageIndex,
			},
			fetcher: &mockFetcher{
				fetchFunc: func(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
					index := ociImageSpecV1.Index{}
					data, _ := json.Marshal(index)
					return io.NopCloser(bytes.NewReader(data)), nil
				},
			},
			expectedError: validate.ErrInvalidComponentVersion,
		},
		{
			name:      "component name mismatch",
			component: "example.com/component",
			tag:       "v1.0.0",
			descriptor: ociImageSpecV1.Descriptor{
				MediaType: ociImageSpecV1.MediaTypeImageManifest,
			},
			fetcher: &mockFetcher{
				fetchFunc: func(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
					manifest := ociImageSpecV1.Manifest{
						Annotations: map[string]string{
							annotations.OCMComponentVersion: annotations.NewComponentVersionAnnotation("example.com/different", "v1.0.0"),
						},
					}
					data, _ := json.Marshal(manifest)
					return io.NopCloser(bytes.NewReader(data)), nil
				},
			},
			expectedError: validate.ErrInvalidComponentVersion,
		},
		{
			name:      "malformed annotation",
			component: "example.com/component",
			tag:       "v1.0.0",
			descriptor: ociImageSpecV1.Descriptor{
				MediaType: ociImageSpecV1.MediaTypeImageManifest,
			},
			fetcher: &mockFetcher{
				fetchFunc: func(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
					manifest := ociImageSpecV1.Manifest{
						Annotations: map[string]string{
							annotations.OCMComponentVersion: "malformed-no-colon",
						},
					}
					data, _ := json.Marshal(manifest)
					return io.NopCloser(bytes.NewReader(data)), nil
				},
			},
			expectedError: fmt.Errorf("failed to parse component version annotation"),
		},
		{
			name:      "fetch error",
			component: "example.com/component",
			tag:       "v1.0.0",
			descriptor: ociImageSpecV1.Descriptor{
				MediaType: ociImageSpecV1.MediaTypeImageManifest,
			},
			fetcher: &mockFetcher{
				fetchFunc: func(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
					return nil, fmt.Errorf("network error")
				},
			},
			expectedError: fmt.Errorf("failed to fetch descriptor"),
		},
		{
			name:      "malformed manifest JSON",
			component: "example.com/component",
			tag:       "v1.0.0",
			descriptor: ociImageSpecV1.Descriptor{
				MediaType: ociImageSpecV1.MediaTypeImageManifest,
			},
			fetcher: &mockFetcher{
				fetchFunc: func(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
					return io.NopCloser(bytes.NewReader([]byte("not json"))), nil
				},
			},
			expectedError: fmt.Errorf("failed to decode manifest"),
		},
		{
			name:      "malformed index JSON",
			component: "example.com/component",
			tag:       "v1.0.0",
			descriptor: ociImageSpecV1.Descriptor{
				MediaType: ociImageSpecV1.MediaTypeImageIndex,
			},
			fetcher: &mockFetcher{
				fetchFunc: func(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
					return io.NopCloser(bytes.NewReader([]byte("not json"))), nil
				},
			},
			expectedError: fmt.Errorf("failed to decode index"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()

			version, err := validate.ComponentVersionDescriptor(
				ctx,
				tt.fetcher,
				tt.descriptor,
				tt.component,
				tt.tag,
			)

			if tt.expectedError != nil {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError.Error())
				if errors.Is(tt.expectedError, validate.ErrInvalidComponentVersion) {
					assert.ErrorIs(t, err, validate.ErrInvalidComponentVersion)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedVersion, version)
			}
		})
	}
}
