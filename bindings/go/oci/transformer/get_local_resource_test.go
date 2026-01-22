package transformer

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	blobaccess "ocm.software/open-component-model/bindings/go/blob/spec/access"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ctfspec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocispec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// mockRepositoryForGet implements ComponentVersionRepository for testing GetLocalResource
type mockRepositoryForGet struct {
	returnBlob     blob.ReadOnlyBlob
	returnResource *descriptor.Resource
}

func (m *mockRepositoryForGet) AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error {
	return nil
}

func (m *mockRepositoryForGet) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	return nil, nil
}

func (m *mockRepositoryForGet) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	return nil, nil
}

func (m *mockRepositoryForGet) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	return nil, nil
}

func (m *mockRepositoryForGet) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	return m.returnBlob, m.returnResource, nil
}

func (m *mockRepositoryForGet) AddLocalSource(ctx context.Context, component, version string, src *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	return nil, nil
}

func (m *mockRepositoryForGet) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	return nil, nil, nil
}

// mockRepoProviderForGet implements ComponentVersionRepositoryProvider for testing GetLocalResource
type mockRepoProviderForGet struct {
	repo *mockRepositoryForGet
}

func (m *mockRepoProviderForGet) GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	return nil, nil
}

func (m *mockRepoProviderForGet) GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (repository.ComponentVersionRepository, error) {
	return m.repo, nil
}

func (m *mockRepoProviderForGet) GetJSONSchemaForRepositorySpecification(typ runtime.Type) ([]byte, error) {
	return nil, nil
}

func TestGetLocalResource_Transform_OCI(t *testing.T) {
	ctx := context.Background()

	// Setup test data - create a blob that the repository will return
	testBlobData := []byte("test resource content from repository")
	testBlob := inmemory.New(bytes.NewReader(testBlobData))
	testBlob.SetMediaType("application/test")

	// Create test resource that the repository will return
	testResource := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-resource",
				Version: "1.0.0",
			},
		},
		Type:     "helmChart",
		Relation: descriptor.LocalRelation,
		Access: &v2.LocalBlob{
			Type: runtime.Type{
				Name:    v2.LocalBlobAccessType,
				Version: v2.LocalBlobAccessTypeVersion,
			},
			MediaType:      "application/test",
			LocalReference: "sha256:test-digest",
		},
	}

	mockRepo := &mockRepositoryForGet{
		returnBlob:     testBlob,
		returnResource: testResource,
	}
	mockProvider := &mockRepoProviderForGet{repo: mockRepo}

	// Create a combined scheme with both v1alpha1 and v2 types
	combinedScheme := runtime.NewScheme()
	v2.MustAddToScheme(combinedScheme)
	blobaccess.MustAddToScheme(combinedScheme)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.OCIGetLocalResource{}, v1alpha1.OCIGetLocalResourceV1alpha1)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.CTFGetLocalResource{}, v1alpha1.CTFGetLocalResourceV1alpha1)

	transformer := &GetLocalResource{
		Scheme:       combinedScheme,
		RepoProvider: mockProvider,
	}

	// Create temporary directory for output
	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "downloaded-resource.bin")

	// Create transformation spec
	spec := &v1alpha1.OCIGetLocalResource{
		Type: runtime.NewVersionedType(v1alpha1.OCIGetLocalResourceType, v1alpha1.Version),
		ID:   "test-get-transform",
		Spec: &v1alpha1.OCIGetLocalResourceSpec{
			Repository: ocispec.Repository{
				Type: runtime.Type{
					Name:    ocispec.Type,
					Version: "v1",
				},
				BaseUrl: "ghcr.io/test/components",
			},
			Component: "ocm.software/test-component",
			Version:   "1.0.0",
			ResourceIdentity: runtime.Identity{
				"name":    "test-resource",
				"version": "1.0.0",
			},
			OutputPath: outputPath,
		},
	}

	// Execute transformation
	result, err := transformer.Transform(ctx, spec)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify result
	transformed, ok := result.(*v1alpha1.OCIGetLocalResource)
	require.True(t, ok)
	require.NotNil(t, transformed.Output)
	require.NotNil(t, transformed.Output.Resource)

	// Verify file was created
	assert.FileExists(t, outputPath)

	// Verify file content
	fileContent, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Equal(t, testBlobData, fileContent)

	// Verify file spec in output
	assert.Equal(t, "file://"+outputPath, transformed.Output.File.URI)
	assert.Equal(t, "application/test", transformed.Output.File.MediaType)

	// Verify resource in output
	assert.Equal(t, "test-resource", transformed.Output.Resource.Name)
	assert.Equal(t, "1.0.0", transformed.Output.Resource.Version)
}

func TestGetLocalResource_Transform_CTF(t *testing.T) {
	ctx := context.Background()

	// Setup test data
	testBlobData := []byte("test CTF resource content")
	testBlob := inmemory.New(bytes.NewReader(testBlobData))

	testResource := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "ctf-resource",
				Version: "2.0.0",
			},
		},
		Type:     "blob",
		Relation: descriptor.LocalRelation,
		Access: &v2.LocalBlob{
			Type: runtime.Type{
				Name:    v2.LocalBlobAccessType,
				Version: v2.LocalBlobAccessTypeVersion,
			},
			MediaType:      "application/octet-stream",
			LocalReference: "sha256:test-ctf-digest",
		},
	}

	mockRepo := &mockRepositoryForGet{
		returnBlob:     testBlob,
		returnResource: testResource,
	}
	mockProvider := &mockRepoProviderForGet{repo: mockRepo}

	combinedScheme := runtime.NewScheme()
	v2.MustAddToScheme(combinedScheme)
	blobaccess.MustAddToScheme(combinedScheme)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.OCIGetLocalResource{}, v1alpha1.OCIGetLocalResourceV1alpha1)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.CTFGetLocalResource{}, v1alpha1.CTFGetLocalResourceV1alpha1)

	transformer := &GetLocalResource{
		Scheme:       combinedScheme,
		RepoProvider: mockProvider,
	}

	// Create transformation spec without OutputPath (should create temp file)
	spec := &v1alpha1.CTFGetLocalResource{
		Type: runtime.NewVersionedType(v1alpha1.CTFGetLocalResourceType, v1alpha1.Version),
		ID:   "test-ctf-get-transform",
		Spec: &v1alpha1.CTFGetLocalResourceSpec{
			Repository: ctfspec.Repository{
				Type: runtime.Type{
					Name:    ctfspec.Type,
					Version: "v1",
				},
				FilePath: "/tmp/test-archive.tar",
			},
			Component: "ocm.software/ctf-component",
			Version:   "2.0.0",
			ResourceIdentity: runtime.Identity{
				"name":    "ctf-resource",
				"version": "2.0.0",
			},
			// OutputPath omitted - should create temp file
		},
	}

	// Execute transformation
	result, err := transformer.Transform(ctx, spec)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify result
	transformed, ok := result.(*v1alpha1.CTFGetLocalResource)
	require.True(t, ok)
	require.NotNil(t, transformed.Output)

	// Verify temp file was created
	outputPath := transformed.Output.File.URI
	assert.Contains(t, outputPath, "file://")
	assert.Contains(t, outputPath, "resource-")

	// Clean up temp file
	tempPath := outputPath[7:] // Remove "file://" prefix
	if _, err := os.Stat(tempPath); err == nil {
		os.Remove(tempPath)
	}
}

func TestGetLocalResource_Transform_ValidationErrors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		spec        *v1alpha1.OCIGetLocalResourceSpec
		expectedErr string
	}{
		{
			name: "missing component",
			spec: &v1alpha1.OCIGetLocalResourceSpec{
				Component:        "",
				Version:          "1.0.0",
				ResourceIdentity: runtime.Identity{"name": "test"},
			},
			expectedErr: "component name is required",
		},
		{
			name: "missing version",
			spec: &v1alpha1.OCIGetLocalResourceSpec{
				Component:        "test",
				Version:          "",
				ResourceIdentity: runtime.Identity{"name": "test"},
			},
			expectedErr: "component version is required",
		},
		{
			name: "missing resource identity",
			spec: &v1alpha1.OCIGetLocalResourceSpec{
				Component:        "test",
				Version:          "1.0.0",
				ResourceIdentity: nil,
			},
			expectedErr: "resource identity is required",
		},
		{
			name: "empty resource identity",
			spec: &v1alpha1.OCIGetLocalResourceSpec{
				Component:        "test",
				Version:          "1.0.0",
				ResourceIdentity: runtime.Identity{},
			},
			expectedErr: "resource identity is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &mockRepositoryForGet{}
			mockProvider := &mockRepoProviderForGet{repo: mockRepo}

			combinedScheme := runtime.NewScheme()
			v2.MustAddToScheme(combinedScheme)
			blobaccess.MustAddToScheme(combinedScheme)
			combinedScheme.MustRegisterWithAlias(&v1alpha1.OCIGetLocalResource{}, v1alpha1.OCIGetLocalResourceV1alpha1)
			combinedScheme.MustRegisterWithAlias(&v1alpha1.CTFGetLocalResource{}, v1alpha1.CTFGetLocalResourceV1alpha1)

			transformer := &GetLocalResource{
				Scheme:       combinedScheme,
				RepoProvider: mockProvider,
			}

			spec := &v1alpha1.OCIGetLocalResource{
				Type: runtime.NewVersionedType(v1alpha1.OCIGetLocalResourceType, v1alpha1.Version),
				Spec: tt.spec,
			}

			result, err := transformer.Transform(ctx, spec)
			assert.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}
