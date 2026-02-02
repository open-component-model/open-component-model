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
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// mockRepositoryForAddOCI implements ComponentVersionRepository and ResourceRepository for testing AddOCIArtifact
type mockRepositoryForAddOCI struct {
	repository.ComponentVersionRepository
	addedResource   *descriptor.Resource
	uploadedBlob    blob.ReadOnlyBlob
	addedLocalBlob  blob.ReadOnlyBlob
	component       string
	version         string
	uploadedToImage string
}

func (m *mockRepositoryForAddOCI) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	m.component = component
	m.version = version
	m.addedResource = res
	m.addedLocalBlob = content

	// Return updated resource with LocalBlob access (for CTF)
	updated := res.DeepCopy()
	updated.Access = &v2.LocalBlob{
		Type: runtime.Type{
			Name:    v2.LocalBlobAccessType,
			Version: v2.LocalBlobAccessTypeVersion,
		},
		MediaType:      "application/vnd.oci.image.manifest.v1+tar+gzip",
		LocalReference: "sha256:test-oci-digest",
	}
	return updated, nil
}

func (m *mockRepositoryForAddOCI) UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	m.addedResource = res
	m.uploadedBlob = content

	// Extract target reference from access
	if rawAccess, ok := res.Access.(*runtime.Raw); ok {
		// Parse the imageReference from access data
		m.uploadedToImage = "parsed-from-access"

		// Return updated resource with ociArtifact access
		updated := res.DeepCopy()
		updated.Access = &runtime.Raw{
			Type: runtime.Type{Name: "ociArtifact"},
			Data: rawAccess.Data,
		}
		return updated, nil
	}

	// Fallback if not raw access
	updated := res.DeepCopy()
	return updated, nil
}

func (m *mockRepositoryForAddOCI) DownloadResource(ctx context.Context, res *descriptor.Resource) (blob.ReadOnlyBlob, error) {
	return nil, nil
}

// mockRepoProviderForAddOCI implements ComponentVersionRepositoryProvider for testing AddOCIArtifact
type mockRepoProviderForAddOCI struct {
	repo *mockRepositoryForAddOCI
}

func (m *mockRepoProviderForAddOCI) GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	return nil, nil
}

func (m *mockRepoProviderForAddOCI) GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (repository.ComponentVersionRepository, error) {
	return m.repo, nil
}

func (m *mockRepoProviderForAddOCI) GetJSONSchemaForRepositorySpecification(typ runtime.Type) ([]byte, error) {
	return nil, nil
}

func TestAddOCIArtifact_Transform_OCI(t *testing.T) {
	ctx := context.Background()

	// Create temporary file with test OCI artifact data (simulating tar archive)
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "oci-artifact.tar")
	testBlobData := []byte("test oci artifact tar content")
	err := os.WriteFile(testFile, testBlobData, 0644)
	require.NoError(t, err)

	mockRepo := &mockRepositoryForAddOCI{}
	mockProvider := &mockRepoProviderForAddOCI{repo: mockRepo}

	// Create a combined scheme
	combinedScheme := runtime.NewScheme()
	v2.MustAddToScheme(combinedScheme)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.OCIAddOCIArtifact{}, v1alpha1.OCIAddOCIArtifactV1alpha1)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.CTFAddOCIArtifact{}, v1alpha1.CTFAddOCIArtifactV1alpha1)

	transformer := &AddOCIArtifact{
		Scheme:       combinedScheme,
		RepoProvider: mockProvider,
	}

	// Create transformation spec
	spec := &v1alpha1.OCIAddOCIArtifact{
		Type: runtime.NewVersionedType(v1alpha1.OCIAddOCIArtifactType, v1alpha1.Version),
		ID:   "test-add-oci-transform",
		Spec: &v1alpha1.OCIAddOCIArtifactSpec{
			TargetReference: "ghcr.io/target/nginx:1.21.0",
			Resource: &v2.Resource{
				ElementMeta: v2.ElementMeta{
					ObjectMeta: v2.ObjectMeta{
						Name:    "nginx-image",
						Version: "1.21.0",
					},
				},
				Type:     "ociArtifact",
				Relation: v2.ExternalRelation,
				Access: &runtime.Raw{
					Type: runtime.Type{Name: "ociArtifact"},
					Data: []byte(`{"type":"ociArtifact","imageReference":"ghcr.io/source/nginx:1.21.0"}`),
				},
			},
			File: blobv1alpha1.File{
				Type: runtime.Type{
					Name:    blobv1alpha1.FileType,
					Version: blobv1alpha1.Version,
				},
				URI:       "file://" + testFile,
				MediaType: "application/vnd.oci.image.manifest.v1+tar+gzip",
			},
		},
	}

	// Execute transformation
	result, err := transformer.Transform(ctx, spec)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify result
	transformed, ok := result.(*v1alpha1.OCIAddOCIArtifact)
	require.True(t, ok)
	require.NotNil(t, transformed.Output)
	require.NotNil(t, transformed.Output.Resource)

	// Verify updated resource has ociArtifact access
	assert.Equal(t, "ociArtifact", transformed.Output.Resource.Access.GetType().Name)

	// Verify repository interactions
	assert.NotNil(t, mockRepo.addedResource)
	assert.Equal(t, "nginx-image", mockRepo.addedResource.Name)
	assert.NotNil(t, mockRepo.uploadedBlob)
}

func TestAddOCIArtifact_Transform_CTF(t *testing.T) {
	ctx := context.Background()

	// Create temporary file with test OCI artifact data
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "oci-artifact-ctf.tar")
	testBlobData := []byte("test oci artifact for CTF")
	err := os.WriteFile(testFile, testBlobData, 0644)
	require.NoError(t, err)

	mockRepo := &mockRepositoryForAddOCI{}
	mockProvider := &mockRepoProviderForAddOCI{repo: mockRepo}

	// Create a combined scheme
	combinedScheme := runtime.NewScheme()
	v2.MustAddToScheme(combinedScheme)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.OCIAddOCIArtifact{}, v1alpha1.OCIAddOCIArtifactV1alpha1)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.CTFAddOCIArtifact{}, v1alpha1.CTFAddOCIArtifactV1alpha1)

	transformer := &AddOCIArtifact{
		Scheme:       combinedScheme,
		RepoProvider: mockProvider,
	}

	// Create CTF transformation spec (no targetReference for CTF)
	spec := &v1alpha1.CTFAddOCIArtifact{
		Type: runtime.NewVersionedType(v1alpha1.CTFAddOCIArtifactType, v1alpha1.Version),
		ID:   "test-ctf-add-oci-transform",
		Spec: &v1alpha1.CTFAddOCIArtifactSpec{
			Repository: ctfspec.Repository{
				Type: runtime.Type{
					Name:    ctfspec.Type,
					Version: "v1",
				},
				FilePath: "/tmp/test-ctf-archive.tar",
			},
			Component: "ocm.software/ctf-component",
			Version:   "2.0.0",
			Resource: &v2.Resource{
				ElementMeta: v2.ElementMeta{
					ObjectMeta: v2.ObjectMeta{
						Name:    "helm-chart",
						Version: "3.0.0",
					},
				},
				Type:     "helmChart",
				Relation: v2.ExternalRelation,
				Access: &runtime.Raw{
					Type: runtime.Type{Name: "ociArtifact"},
					Data: []byte(`{"type":"ociArtifact","imageReference":"registry.example.com/charts/app:3.0.0"}`),
				},
			},
			File: blobv1alpha1.File{
				Type: runtime.Type{
					Name:    blobv1alpha1.FileType,
					Version: blobv1alpha1.Version,
				},
				URI:       "file://" + testFile,
				MediaType: "application/vnd.oci.image.manifest.v1+tar+gzip",
			},
		},
	}

	// Execute transformation
	result, err := transformer.Transform(ctx, spec)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify result
	transformed, ok := result.(*v1alpha1.CTFAddOCIArtifact)
	require.True(t, ok)
	require.NotNil(t, transformed.Output)
	require.NotNil(t, transformed.Output.Resource)

	// Verify resource was converted to local blob for CTF
	assert.Equal(t, v2.LocalBlobAccessType, transformed.Output.Resource.Access.GetType().Name)

	// Verify repository interactions - should use AddLocalResource for CTF
	assert.Equal(t, "ocm.software/ctf-component", mockRepo.component)
	assert.Equal(t, "2.0.0", mockRepo.version)
	assert.NotNil(t, mockRepo.addedLocalBlob)
}

func TestAddOCIArtifact_Transform_ValidationErrors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		spec        *v1alpha1.OCIAddOCIArtifactSpec
		expectedErr string
	}{
		{
			name: "missing resource",
			spec: &v1alpha1.OCIAddOCIArtifactSpec{
				Resource:        nil,
				TargetReference: "ghcr.io/test/image:v1",
				File:            blobv1alpha1.File{URI: "file:///tmp/test"},
			},
			expectedErr: "resource is required",
		},
		{
			name: "missing file URI",
			spec: &v1alpha1.OCIAddOCIArtifactSpec{
				Resource:        &v2.Resource{},
				TargetReference: "ghcr.io/test/image:v1",
				File: blobv1alpha1.File{
					URI: "",
				},
			},
			expectedErr: "file URI is required",
		},
		{
			name: "missing target reference for OCI",
			spec: &v1alpha1.OCIAddOCIArtifactSpec{
				Resource:        &v2.Resource{},
				TargetReference: "",
				File:            blobv1alpha1.File{URI: "file:///tmp/test"},
			},
			expectedErr: "targetReference is required for OCI repository uploads",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &mockRepositoryForAddOCI{}
			mockProvider := &mockRepoProviderForAddOCI{repo: mockRepo}

			combinedScheme := runtime.NewScheme()
			v2.MustAddToScheme(combinedScheme)
			combinedScheme.MustRegisterWithAlias(&v1alpha1.OCIAddOCIArtifact{}, v1alpha1.OCIAddOCIArtifactV1alpha1)

			transformer := &AddOCIArtifact{
				Scheme:       combinedScheme,
				RepoProvider: mockProvider,
			}

			spec := &v1alpha1.OCIAddOCIArtifact{
				Type: runtime.NewVersionedType(v1alpha1.OCIAddOCIArtifactType, v1alpha1.Version),
				Spec: tt.spec,
			}

			result, err := transformer.Transform(ctx, spec)
			assert.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}
