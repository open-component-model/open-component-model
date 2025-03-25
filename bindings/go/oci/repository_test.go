package oci_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content/memory"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// MockResolver implements the Resolver interface for testing
type MockResolver struct {
	store oci.Store
}

func (m *MockResolver) StoreForReference(ctx context.Context, reference string) (oci.Store, error) {
	return m.store, nil
}

func (m *MockResolver) ComponentVersionReference(component, version string) string {
	return component + ":" + version
}

func (m *MockResolver) TargetResourceReference(srcReference string) (string, error) {
	return srcReference, nil
}

func TestRepository_AddComponentVersion(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	mockStore := memory.New()

	mockResolver := &MockResolver{store: mockStore}
	repo := oci.RepositoryFromResolverAndMemory(mockResolver, oci.NewLocalBlobMemory())

	// Create a test component descriptor
	desc := &descriptor.Descriptor{
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-component",
					Version: "1.0.0",
				},
			},
		},
	}

	// Test adding component version
	err := repo.AddComponentVersion(ctx, desc)
	r.NoError(err, "Failed to add component version when it should succeed")

	resolved, err := mockStore.Resolve(ctx, mockResolver.ComponentVersionReference(desc.Component.Name, desc.Component.Version))
	r.NoError(err, "Failed to resolve component version after adding it")

	r.NotNil(resolved, "Resolved component version should not be nil")
}

func TestRepository_GetComponentVersion(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	mockStore := memory.New()
	mockResolver := &MockResolver{store: mockStore}
	repo := oci.RepositoryFromResolverAndMemory(mockResolver, oci.NewLocalBlobMemory())

	// Test getting non-existent component version
	desc, err := repo.GetComponentVersion(ctx, "test-component", "1.0.0")
	r.Error(err, "Expected error when getting non-existent component version")
	r.Nil(desc, "Expected nil descriptor when getting non-existent component version")

	// Create a test component descriptor
	desc = &descriptor.Descriptor{
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-component",
					Version: "1.0.0",
				},
			},
		},
	}
	// Test adding component version
	err = repo.AddComponentVersion(ctx, desc)
	r.NoError(err, "Failed to add component version when it should succeed")

	desc, err = repo.GetComponentVersion(ctx, "test-component", "1.0.0")
	r.NoError(err, "Expected error when getting non-existent component version")
	r.NotNil(desc, "Expected nil descriptor when getting non-existent component version")

}

func TestRepository_AddLocalResource(t *testing.T) {
	ctx := context.Background()
	mockStore := memory.New()
	mockResolver := &MockResolver{store: mockStore}
	repo := oci.RepositoryFromResolverAndMemory(mockResolver, oci.NewLocalBlobMemory())

	// Create a test resource
	resource := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-resource",
				Version: "1.0.0",
			},
		},
		Type: "test-type",
	}

	// Create a test blob
	testData := []byte("test data")
	b := blob.NewDirectReadOnlyBlob(bytes.NewReader(testData))

	// Test adding local resource
	newRes, err := repo.AddLocalResource(ctx, "test-component", "1.0.0", resource, b)
	require.NoError(t, err)
	assert.NotNil(t, newRes)
}

func TestRepository_GetLocalResource(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	// Create test resources with different configurations
	testCases := []struct {
		name           string
		resource       *descriptor.Resource
		content        []byte
		identity       map[string]string
		expectError    bool
		errorContains  string
		setupComponent bool
	}{
		{
			name: "non-existent component",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-resource",
						Version: "1.0.0",
					},
				},
				Type: "test-type",
				Access: &runtime.Raw{
					Type: runtime.Type{
						Name: "localBlob",
					},
					Data: []byte(`{"localReference":"sha256:1234567890","mediaType":"application/octet-stream"}`),
				},
			},
			identity: map[string]string{
				"name":    "test-resource",
				"version": "1.0.0",
			},
			expectError:    true,
			setupComponent: false,
		},
		{
			name: "non-existent resource in existing component",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-resource",
						Version: "1.0.0",
					},
				},
				Type: "test-type",
				Access: &runtime.Raw{
					Type: runtime.Type{
						Name: "localBlob",
					},
					Data: []byte(`{"localReference":"sha256:1234567890","mediaType":"application/octet-stream"}`),
				},
			},
			identity: map[string]string{
				"name":    "test-resource",
				"version": "1.0.0",
			},
			expectError:    true,
			setupComponent: true,
		},
		{
			name: "resource with platform-specific identity",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "platform-resource",
						Version: "1.0.0",
					},
					ExtraIdentity: map[string]string{
						"architecture": "amd64",
						"os":           "linux",
					},
				},
				Type: "test-type",
				Access: &runtime.Raw{
					Type: v2.GetLocalBlobAccessType(),
					Data: []byte(fmt.Sprintf(
						`{"type":"%s","localReference":"%s","mediaType":"application/octet-stream"}`,
						v2.GetLocalBlobAccessType().String(),
						digest.FromString("platform specific content").String(),
					)),
				},
			},
			content: []byte("platform specific content"),
			identity: map[string]string{
				"name":         "platform-resource",
				"version":      "1.0.0",
				"architecture": "amd64",
				"os":           "linux",
			},
			expectError:    false,
			setupComponent: true,
		},
		{
			name: "resource with invalid identity",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-resource",
						Version: "1.0.0",
					},
				},
				Type: "test-type",
				Access: &runtime.Raw{
					Type: runtime.Type{
						Name: "localBlob",
					},
					Data: []byte(`{"localReference":"sha256:1234567890","mediaType":"application/octet-stream"}`),
				},
			},
			identity: map[string]string{
				"invalid": "key",
			},
			expectError:    true,
			setupComponent: true,
		},
		{
			name: "resource with invalid identity",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-resource",
						Version: "1.0.0",
					},
				},
				Type: "test-type",
				Access: &runtime.Raw{
					Type: runtime.Type{
						Name: "invalidAccess",
					},
					Data: []byte(`{"invalid":"data"}`),
				},
			},
			identity: map[string]string{
				"name":    "test-resource",
				"version": "1.0.0",
			},
			expectError:    true,
			errorContains:  "no matching layers for identity map[name:test-resource version:1.0.0] (not matched other layers []): not found",
			setupComponent: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockStore := memory.New()
			mockResolver := &MockResolver{store: mockStore}
			repo := oci.RepositoryFromResolverAndMemory(mockResolver, oci.NewLocalBlobMemory())

			// Create a test component descriptor
			desc := &descriptor.Descriptor{
				Component: descriptor.Component{
					ComponentMeta: descriptor.ComponentMeta{
						ObjectMeta: descriptor.ObjectMeta{
							Name:    "test-component",
							Version: "1.0.0",
						},
					},
				},
			}

			// Add resource if content is provided
			if tc.content != nil {
				b := blob.NewDirectReadOnlyBlob(bytes.NewReader(tc.content))
				newRes, err := repo.AddLocalResource(ctx, desc.Component.Name, desc.Component.Version, tc.resource, b)
				r.NoError(err, "Failed to add test resource")
				r.NotNil(newRes, "Resource should not be nil after adding")
			}

			// Setup component if needed
			if tc.setupComponent {
				err := repo.AddComponentVersion(ctx, desc)
				r.NoError(err, "Failed to setup test component")
			}

			// Test getting the resource
			blob, err := repo.GetLocalResource(ctx, desc.Component.Name, desc.Component.Version, tc.identity)

			if tc.expectError {
				r.Error(err, "Expected error but got none")
				if tc.errorContains != "" {
					r.Contains(err.Error(), tc.errorContains, "Error message should contain expected text")
				}
				r.Nil(blob, "Blob should be nil when error occurs")
			} else {
				r.NoError(err, "Unexpected error when getting resource")
				r.NotNil(blob, "Blob should not be nil for successful retrieval")

				// Verify blob content if it was provided
				if tc.content != nil {
					reader, err := blob.ReadCloser()
					r.NoError(err, "Failed to get blob reader")
					defer reader.Close()

					content, err := io.ReadAll(reader)
					r.NoError(err, "Failed to read blob content")
					r.Equal(tc.content, content, "Blob content should match expected content")
				}
			}
		})
	}
}

func TestRepository_UploadResource(t *testing.T) {
	ctx := context.Background()
	mockStore := memory.New()
	mockResolver := &MockResolver{store: mockStore}
	repo := oci.RepositoryFromResolverAndMemory(mockResolver, oci.NewLocalBlobMemory())

	// Create a test resource
	resource := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-resource",
				Version: "1.0.0",
			},
		},
		Type: "test-type",
	}

	// Create a test blob
	testData := []byte("test data")
	blob := blob.NewDirectReadOnlyBlob(bytes.NewReader(testData))

	// Test uploading resource
	newRes, err := repo.UploadResource(ctx, resource, blob)
	require.NoError(t, err)
	assert.NotNil(t, newRes)
}

func TestRepository_DownloadResource(t *testing.T) {
	ctx := context.Background()
	mockStore := memory.New()
	mockResolver := &MockResolver{store: mockStore}
	repo := oci.RepositoryFromResolverAndMemory(mockResolver, oci.NewLocalBlobMemory())

	// Create a test resource
	resource := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-resource",
				Version: "1.0.0",
			},
		},
		Type: "test-type",
	}

	// Test downloading non-existent resource
	blob, err := repo.DownloadResource(ctx, resource)
	assert.Error(t, err)
	assert.Nil(t, blob)
}
