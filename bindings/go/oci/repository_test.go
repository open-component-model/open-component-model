package oci_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content/memory"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
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
	ctx := context.Background()
	mockStore := memory.New()
	mockResolver := &MockResolver{store: mockStore}
	repo := oci.RepositoryFromResolverAndMemory(mockResolver, oci.NewLocalBlobMemory())

	// Test getting non-existent local resource
	identity := map[string]string{
		"name":    "test-resource",
		"version": "1.0.0",
	}
	blob, err := repo.GetLocalResource(ctx, "test-component", "1.0.0", identity)
	assert.Error(t, err)
	assert.Nil(t, blob)
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
