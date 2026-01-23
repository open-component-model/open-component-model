package transformer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	blobv1alpha1 "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ctfspec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocispec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// mockRepository implements ComponentVersionRepository for testing
type mockRepository struct {
	addedResource *descriptor.Resource
	addedBlob     blob.ReadOnlyBlob
	component     string
	version       string
}

func (m *mockRepository) AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error {
	return nil
}

func (m *mockRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	return nil, nil
}

func (m *mockRepository) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	return nil, nil
}

func (m *mockRepository) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	m.component = component
	m.version = version
	m.addedResource = res
	m.addedBlob = content

	// Return updated resource with LocalBlob access
	updated := res.DeepCopy()
	updated.Access = &v2.LocalBlob{
		Type: runtime.Type{
			Name:    v2.LocalBlobAccessType,
			Version: v2.LocalBlobAccessTypeVersion,
		},
		MediaType:      "application/octet-stream",
		LocalReference: "sha256:test-digest",
	}
	return updated, nil
}

func (m *mockRepository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	return nil, nil, nil
}

func (m *mockRepository) AddLocalSource(ctx context.Context, component, version string, src *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	return nil, nil
}

func (m *mockRepository) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	return nil, nil, nil
}

// mockRepoProvider implements ComponentVersionRepositoryProvider for testing
type mockRepoProvider struct {
	repo *mockRepository
}

func (m *mockRepoProvider) GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	return nil, nil
}

func (m *mockRepoProvider) GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (repository.ComponentVersionRepository, error) {
	return m.repo, nil
}

func (m *mockRepoProvider) GetJSONSchemaForRepositorySpecification(typ runtime.Type) ([]byte, error) {
	return nil, nil
}

func TestAddLocalResource_Transform_OCI(t *testing.T) {
	ctx := context.Background()

	// Create temporary file with test data
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test-resource.bin")
	testBlobData := []byte("test blob content")
	err := os.WriteFile(testFile, testBlobData, 0644)
	require.NoError(t, err)

	mockRepo := &mockRepository{}
	mockProvider := &mockRepoProvider{repo: mockRepo}

	// Create a combined scheme with both v1alpha1 and v2 types
	combinedScheme := runtime.NewScheme()
	v2.MustAddToScheme(combinedScheme)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.OCIAddLocalResource{}, v1alpha1.OCIAddLocalResourceV1alpha1)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.CTFAddLocalResource{}, v1alpha1.CTFAddLocalResourceV1alpha1)

	transformer := &AddLocalResource{
		Scheme:       combinedScheme,
		RepoProvider: mockProvider,
	}

	// Create transformation spec
	spec := &v1alpha1.OCIAddLocalResource{
		Type: runtime.NewVersionedType(v1alpha1.OCIAddLocalResourceType, v1alpha1.Version),
		ID:   "test-transform",
		Spec: &v1alpha1.OCIAddLocalResourceSpec{
			Repository: ocispec.Repository{
				Type: runtime.Type{
					Name:    ocispec.Type,
					Version: "v1",
				},
				BaseUrl: "ghcr.io/test/components",
			},
			Component: "ocm.software/test-component",
			Version:   "1.0.0",
			Resource: &v2.Resource{
				ElementMeta: v2.ElementMeta{
					ObjectMeta: v2.ObjectMeta{
						Name:    "test-resource",
						Version: "1.0.0",
					},
				},
				Type:     "helmChart",
				Relation: v2.LocalRelation,
			},
			File: blobv1alpha1.File{
				Type: runtime.Type{
					Name:    blobv1alpha1.FileType,
					Version: blobv1alpha1.Version,
				},
				URI:       "file://" + testFile,
				MediaType: "application/test",
			},
		},
	}

	// Execute transformation
	result, err := transformer.Transform(ctx, spec)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify result
	transformed, ok := result.(*v1alpha1.OCIAddLocalResource)
	require.True(t, ok)
	require.NotNil(t, transformed.Output)
	require.NotNil(t, transformed.Output.Resource)

	// Verify updated resource has LocalBlob access
	assert.Equal(t, v2.LocalBlobAccessType, transformed.Output.Resource.Access.GetType().Name)

	// Verify repository interactions
	assert.Equal(t, "ocm.software/test-component", mockRepo.component)
	assert.Equal(t, "1.0.0", mockRepo.version)
	assert.NotNil(t, mockRepo.addedResource)
	assert.Equal(t, "test-resource", mockRepo.addedResource.Name)
}

func TestAddLocalResource_Transform_CTF(t *testing.T) {
	ctx := context.Background()

	// Create temporary file with test data
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test-ctf-resource.bin")
	testBlobData := []byte("test blob content for CTF")
	err := os.WriteFile(testFile, testBlobData, 0644)
	require.NoError(t, err)

	mockRepo := &mockRepository{}
	mockProvider := &mockRepoProvider{repo: mockRepo}

	// Create a combined scheme with both v1alpha1 and v2 types
	combinedScheme := runtime.NewScheme()
	v2.MustAddToScheme(combinedScheme)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.OCIAddLocalResource{}, v1alpha1.OCIAddLocalResourceV1alpha1)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.CTFAddLocalResource{}, v1alpha1.CTFAddLocalResourceV1alpha1)

	transformer := &AddLocalResource{
		Scheme:       combinedScheme,
		RepoProvider: mockProvider,
	}

	// Create CTF transformation spec
	spec := &v1alpha1.CTFAddLocalResource{
		Type: runtime.NewVersionedType(v1alpha1.CTFAddLocalResourceType, v1alpha1.Version),
		ID:   "test-ctf-transform",
		Spec: &v1alpha1.CTFAddLocalResourceSpec{
			Repository: ctfspec.Repository{
				Type: runtime.Type{
					Name:    ctfspec.Type,
					Version: "v1",
				},
				FilePath: "/tmp/test-archive.tar",
			},
			Component: "ocm.software/ctf-component",
			Version:   "2.0.0",
			Resource: &v2.Resource{
				ElementMeta: v2.ElementMeta{
					ObjectMeta: v2.ObjectMeta{
						Name:    "ctf-resource",
						Version: "2.0.0",
					},
				},
				Type:     "blob",
				Relation: v2.LocalRelation,
			},
			File: blobv1alpha1.File{
				Type: runtime.Type{
					Name:    blobv1alpha1.FileType,
					Version: blobv1alpha1.Version,
				},
				URI:       "file://" + testFile,
				MediaType: "application/octet-stream",
			},
		},
	}

	// Execute transformation
	result, err := transformer.Transform(ctx, spec)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify result
	transformed, ok := result.(*v1alpha1.CTFAddLocalResource)
	require.True(t, ok)
	require.NotNil(t, transformed.Output)
	require.NotNil(t, transformed.Output.Resource)

	// Verify repository interactions
	assert.Equal(t, "ocm.software/ctf-component", mockRepo.component)
	assert.Equal(t, "2.0.0", mockRepo.version)
}

func TestAddLocalResource_Transform_ValidationErrors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		spec        *v1alpha1.OCIAddLocalResourceSpec
		expectedErr string
	}{
		{
			name: "missing component",
			spec: &v1alpha1.OCIAddLocalResourceSpec{
				Component: "",
				Version:   "1.0.0",
				Resource:  &v2.Resource{},
			},
			expectedErr: "component name is required",
		},
		{
			name: "missing version",
			spec: &v1alpha1.OCIAddLocalResourceSpec{
				Component: "test",
				Version:   "",
				Resource:  &v2.Resource{},
			},
			expectedErr: "component version is required",
		},
		{
			name: "missing resource",
			spec: &v1alpha1.OCIAddLocalResourceSpec{
				Component: "test",
				Version:   "1.0.0",
				Resource:  nil,
			},
			expectedErr: "resource is required",
		},
		{
			name: "missing file URI",
			spec: &v1alpha1.OCIAddLocalResourceSpec{
				Component: "test",
				Version:   "1.0.0",
				Resource:  &v2.Resource{},
				File: blobv1alpha1.File{
					URI: "",
				},
			},
			expectedErr: "file URI is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &mockRepository{}
			mockProvider := &mockRepoProvider{repo: mockRepo}

			// Create a combined scheme with both v1alpha1 and v2 types
			combinedScheme := runtime.NewScheme()
			v2.MustAddToScheme(combinedScheme)
			combinedScheme.MustRegisterWithAlias(&v1alpha1.OCIAddLocalResource{}, v1alpha1.OCIAddLocalResourceV1alpha1)
			combinedScheme.MustRegisterWithAlias(&v1alpha1.CTFAddLocalResource{}, v1alpha1.CTFAddLocalResourceV1alpha1)

			transformer := &AddLocalResource{
				Scheme:       combinedScheme,
				RepoProvider: mockProvider,
			}

			spec := &v1alpha1.OCIAddLocalResource{
				Type: runtime.NewVersionedType(v1alpha1.OCIAddLocalResourceType, v1alpha1.Version),
				Spec: tt.spec,
			}

			result, err := transformer.Transform(ctx, spec)
			assert.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}
