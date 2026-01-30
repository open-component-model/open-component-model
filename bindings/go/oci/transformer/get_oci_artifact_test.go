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
	filesystemaccess "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ctfspec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// mockRepositoryForGetOCI implements ComponentVersionRepository and ResourceRepository for testing GetOCIArtifact
type mockRepositoryForGetOCI struct {
	returnBlob       blob.ReadOnlyBlob
	returnDescriptor *descriptor.Descriptor
}

func (m *mockRepositoryForGetOCI) AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error {
	return nil
}

func (m *mockRepositoryForGetOCI) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	return m.returnDescriptor, nil
}

func (m *mockRepositoryForGetOCI) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	return nil, nil
}

func (m *mockRepositoryForGetOCI) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	return nil, nil
}

func (m *mockRepositoryForGetOCI) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	return nil, nil, nil
}

func (m *mockRepositoryForGetOCI) AddLocalSource(ctx context.Context, component, version string, src *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	return nil, nil
}

func (m *mockRepositoryForGetOCI) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	return nil, nil, nil
}

func (m *mockRepositoryForGetOCI) UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	return nil, nil
}

func (m *mockRepositoryForGetOCI) DownloadResource(ctx context.Context, res *descriptor.Resource) (blob.ReadOnlyBlob, error) {
	return m.returnBlob, nil
}

// mockRepoProviderForGetOCI implements ComponentVersionRepositoryProvider for testing GetOCIArtifact
type mockRepoProviderForGetOCI struct {
	repo *mockRepositoryForGetOCI
}

func (m *mockRepoProviderForGetOCI) GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	return nil, nil
}

func (m *mockRepoProviderForGetOCI) GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (repository.ComponentVersionRepository, error) {
	return m.repo, nil
}

func (m *mockRepoProviderForGetOCI) GetJSONSchemaForRepositorySpecification(typ runtime.Type) ([]byte, error) {
	return nil, nil
}

func TestGetOCIArtifact_Transform_OCI(t *testing.T) {
	ctx := context.Background()

	// Setup test data - create a blob that the repository will return (OCI artifact as tar)
	testBlobData := []byte("test oci artifact content as tar archive")
	testBlob := inmemory.New(bytes.NewReader(testBlobData))
	testBlob.SetMediaType("application/vnd.oci.image.manifest.v1+tar+gzip")

	// Create test resource with ociArtifact access
	testResource := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "nginx-image",
				Version: "1.21.0",
			},
		},
		Type:     "ociArtifact",
		Relation: descriptor.ExternalRelation,
		Access: &runtime.Raw{
			Type: runtime.Type{Name: "ociArtifact"},
			Data: []byte(`{"type":"ociArtifact","imageReference":"ghcr.io/example/nginx:1.21.0"}`),
		},
	}

	// Create descriptor with the test resource
	testDescriptor := &descriptor.Descriptor{
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "ocm.software/test-component",
					Version: "1.0.0",
				},
			},
			Provider: descriptor.Provider{
				Name: "ocm.software",
			},
			Resources: []descriptor.Resource{
				*testResource,
			},
		},
	}

	mockRepo := &mockRepositoryForGetOCI{
		returnBlob:       testBlob,
		returnDescriptor: testDescriptor,
	}
	mockProvider := &mockRepoProviderForGetOCI{repo: mockRepo}

	// Create a combined scheme
	combinedScheme := runtime.NewScheme()
	v2.MustAddToScheme(combinedScheme)
	filesystemaccess.MustAddToScheme(combinedScheme)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.OCIGetOCIArtifact{}, v1alpha1.OCIGetOCIArtifactV1alpha1)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.CTFGetOCIArtifact{}, v1alpha1.CTFGetOCIArtifactV1alpha1)

	transformer := &GetOCIArtifact{
		Scheme:       combinedScheme,
		RepoProvider: mockProvider,
	}

	// Create temporary directory for output
	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "oci-artifact.tar")

	// Create transformation spec
	spec := &v1alpha1.OCIGetOCIArtifact{
		Type: runtime.NewVersionedType(v1alpha1.OCIGetOCIArtifactType, v1alpha1.Version),
		ID:   "test-get-oci-transform",
		Spec: &v1alpha1.OCIGetOCIArtifactSpec{
			ResourceIdentity: runtime.Identity{
				"name":    "nginx-image",
				"version": "1.21.0",
			},
			OutputPath: outputPath,
		},
	}

	// Execute transformation
	result, err := transformer.Transform(ctx, spec)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify result
	transformed, ok := result.(*v1alpha1.OCIGetOCIArtifact)
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

	// Verify resource in output
	assert.Equal(t, "nginx-image", transformed.Output.Resource.Name)
	assert.Equal(t, "1.21.0", transformed.Output.Resource.Version)
}

func TestGetOCIArtifact_Transform_CTF(t *testing.T) {
	ctx := context.Background()

	// Setup test data - CTF with OCI artifact stored as local blob
	testBlobData := []byte("test ctf oci artifact content")
	testBlob := inmemory.New(bytes.NewReader(testBlobData))

	testResource := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "helm-chart",
				Version: "3.0.0",
			},
		},
		Type:     "helmChart",
		Relation: descriptor.ExternalRelation,
		Access: &runtime.Raw{
			Type: runtime.Type{Name: "ociArtifact"},
			Data: []byte(`{"type":"ociArtifact","imageReference":"registry.example.com/charts/app:3.0.0"}`),
		},
	}

	testDescriptor := &descriptor.Descriptor{
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "ocm.software/ctf-component",
					Version: "2.0.0",
				},
			},
			Provider: descriptor.Provider{
				Name: "ocm.software",
			},
			Resources: []descriptor.Resource{
				*testResource,
			},
		},
	}

	mockRepo := &mockRepositoryForGetOCI{
		returnBlob:       testBlob,
		returnDescriptor: testDescriptor,
	}
	mockProvider := &mockRepoProviderForGetOCI{repo: mockRepo}

	combinedScheme := runtime.NewScheme()
	v2.MustAddToScheme(combinedScheme)
	filesystemaccess.MustAddToScheme(combinedScheme)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.OCIGetOCIArtifact{}, v1alpha1.OCIGetOCIArtifactV1alpha1)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.CTFGetOCIArtifact{}, v1alpha1.CTFGetOCIArtifactV1alpha1)

	transformer := &GetOCIArtifact{
		Scheme:       combinedScheme,
		RepoProvider: mockProvider,
	}

	// Create transformation spec without OutputPath (should create temp file)
	spec := &v1alpha1.CTFGetOCIArtifact{
		Type: runtime.NewVersionedType(v1alpha1.CTFGetOCIArtifactType, v1alpha1.Version),
		ID:   "test-ctf-get-oci-transform",
		Spec: &v1alpha1.CTFGetOCIArtifactSpec{
			Repository: ctfspec.Repository{
				Type: runtime.Type{
					Name:    ctfspec.Type,
					Version: "v1",
				},
				FilePath: "/tmp/test-ctf-archive.tar",
			},
			Component: "ocm.software/ctf-component",
			Version:   "2.0.0",
			ResourceIdentity: runtime.Identity{
				"name":    "helm-chart",
				"version": "3.0.0",
			},
			// OutputPath omitted - should create temp file
		},
	}

	// Execute transformation
	result, err := transformer.Transform(ctx, spec)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify result
	transformed, ok := result.(*v1alpha1.CTFGetOCIArtifact)
	require.True(t, ok)
	require.NotNil(t, transformed.Output)

	// Verify temp file was created
	outputPath := transformed.Output.File.URI
	assert.Contains(t, outputPath, "file://")
	assert.Contains(t, outputPath, "oci-artifact-")

	// Clean up temp file
	tempPath := outputPath[7:] // Remove "file://" prefix
	if _, err := os.Stat(tempPath); err == nil {
		os.Remove(tempPath)
	}
}

func TestGetOCIArtifact_Transform_ValidationErrors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		spec        *v1alpha1.OCIGetOCIArtifactSpec
		expectedErr string
	}{
		{
			name: "missing resource identity",
			spec: &v1alpha1.OCIGetOCIArtifactSpec{
				ResourceIdentity: nil,
			},
			expectedErr: "resource identity is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &mockRepositoryForGetOCI{}
			mockProvider := &mockRepoProviderForGetOCI{repo: mockRepo}

			combinedScheme := runtime.NewScheme()
			v2.MustAddToScheme(combinedScheme)
			filesystemaccess.MustAddToScheme(combinedScheme)
			combinedScheme.MustRegisterWithAlias(&v1alpha1.OCIGetOCIArtifact{}, v1alpha1.OCIGetOCIArtifactV1alpha1)

			transformer := &GetOCIArtifact{
				Scheme:       combinedScheme,
				RepoProvider: mockProvider,
			}

			spec := &v1alpha1.OCIGetOCIArtifact{
				Type: runtime.NewVersionedType(v1alpha1.OCIGetOCIArtifactType, v1alpha1.Version),
				Spec: tt.spec,
			}

			result, err := transformer.Transform(ctx, spec)
			assert.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}
