package transformer

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	blobv1alpha1 "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// mockResourceRepositoryForAddOCI implements ResourceRepository for testing AddOCIArtifact
type mockResourceRepositoryForAddOCI struct {
	repository.ResourceRepository
	uploadedResource *descriptor.Resource
	uploadedBlob     blob.ReadOnlyBlob
	creds            map[string]string
}

func (m *mockResourceRepositoryForAddOCI) UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob, credentials map[string]string) (*descriptor.Resource, error) {
	m.uploadedResource = res
	m.uploadedBlob = content
	m.creds = credentials

	// Return updated resource
	updated := res.DeepCopy()

	// Access spec for OCI Image Layer
	access := &ociaccess.OCIImageLayer{
		Type: runtime.Type{
			Name:    ociaccess.LegacyOCIBlobAccessType,
			Version: "v1",
		},
		Reference: "ghcr.io/test/artifact@sha256:test-digest",
		MediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
	}
	accessData, err := json.Marshal(access)
	if err != nil {
		return nil, err
	}

	updated.Access = &runtime.Raw{
		Type: access.Type,
		Data: accessData,
	}
	return updated, nil
}

func (m *mockResourceRepositoryForAddOCI) GetResourceCredentialConsumerIdentity(ctx context.Context, res *descriptor.Resource) (runtime.Identity, error) {
	return runtime.Identity{"type": "ociRegistry", "hostname": "ghcr.io"}, nil
}

// mockCredentialResolver implements credentials.Resolver for testing
type mockCredentialResolver struct{}

func (m *mockCredentialResolver) Resolve(ctx context.Context, id runtime.Identity) (map[string]string, error) {
	return map[string]string{"username": "test-user"}, nil
}

func TestAddOCIArtifact_Transform(t *testing.T) {
	ctx := context.Background()

	// Create temporary file with test data
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test-artifact.tar")
	testBlobData := []byte("test artifact content")
	err := os.WriteFile(testFile, testBlobData, 0644)
	require.NoError(t, err)

	mockRepo := &mockResourceRepositoryForAddOCI{}
	mockCreds := &mockCredentialResolver{}

	// Create a combined scheme
	combinedScheme := runtime.NewScheme()
	v2.MustAddToScheme(combinedScheme)
	// Register AddOCIArtifact
	combinedScheme.MustRegisterWithAlias(&v1alpha1.AddOCIArtifact{}, runtime.NewVersionedType(v1alpha1.AddOCIArtifactType, v1alpha1.AddOCIArtifactVersion))

	transformer := &AddOCIArtifact{
		Scheme:             combinedScheme,
		Repository:         mockRepo,
		CredentialProvider: mockCreds,
	}

	// Create transformation spec
	spec := &v1alpha1.AddOCIArtifact{
		Type: runtime.NewVersionedType(v1alpha1.AddOCIArtifactType, v1alpha1.AddOCIArtifactVersion),
		Spec: &v1alpha1.AddOCIArtifactSpec{
			Resource: &v2.Resource{
				ElementMeta: v2.ElementMeta{
					ObjectMeta: v2.ObjectMeta{
						Name:    "test-artifact",
						Version: "0.1.0",
					},
				},
				Type:     "ociImage",
				Relation: v2.LocalRelation,
			},
			File: blobv1alpha1.File{
				Type: runtime.Type{
					Name:    blobv1alpha1.FileType,
					Version: blobv1alpha1.Version,
				},
				URI:       "file://" + testFile,
				MediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
			},
		},
	}

	// Execute transformation
	result, err := transformer.Transform(ctx, spec)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify result
	transformed, ok := result.(*v1alpha1.AddOCIArtifact)
	require.True(t, ok)
	require.NotNil(t, transformed.Output)
	require.NotNil(t, transformed.Output.Resource)

	// Verify updated resource has OCIImageLayer access
	require.NotNil(t, transformed.Output.Resource.Access)
	var accessSpec ociaccess.OCIImageLayer
	err = json.Unmarshal(transformed.Output.Resource.Access.Data, &accessSpec)
	require.NoError(t, err)

	assert.Equal(t, ociaccess.LegacyOCIBlobAccessType, accessSpec.Type.Name)
	assert.Equal(t, "ghcr.io/test/artifact@sha256:test-digest", accessSpec.Reference)
	assert.Equal(t, "application/vnd.oci.image.layer.v1.tar+gzip", accessSpec.MediaType)

	// Verify repository interactions
	assert.NotNil(t, mockRepo.uploadedResource)
	assert.Equal(t, "test-artifact", mockRepo.uploadedResource.Name)
	assert.Equal(t, "0.1.0", mockRepo.uploadedResource.Version)

	// Verify blob content was passed correctly
	require.NotNil(t, mockRepo.uploadedBlob)
	reader, err := mockRepo.uploadedBlob.ReadCloser()
	require.NoError(t, err)
	defer reader.Close()
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, testBlobData, data)

	// Verify credentials were resolved and passed
	assert.Equal(t, "test-user", mockRepo.creds["username"])
}

func TestAddOCIArtifact_ValidationErrors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		spec        *v1alpha1.AddOCIArtifactSpec
		expectedErr string
	}{
		{
			name: "missing resource",
			spec: &v1alpha1.AddOCIArtifactSpec{
				Resource: nil,
				File: blobv1alpha1.File{
					URI: "file:///tmp/test",
				},
			},
			expectedErr: "resource is required",
		},
		{
			name: "missing file URI",
			spec: &v1alpha1.AddOCIArtifactSpec{
				Resource: &v2.Resource{},
				File: blobv1alpha1.File{
					URI: "",
				},
			},
			expectedErr: "file is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &mockResourceRepositoryForAddOCI{}

			combinedScheme := runtime.NewScheme()
			v2.MustAddToScheme(combinedScheme)
			combinedScheme.MustRegisterWithAlias(&v1alpha1.AddOCIArtifact{}, runtime.NewVersionedType(v1alpha1.AddOCIArtifactType, v1alpha1.AddOCIArtifactVersion))

			transformer := &AddOCIArtifact{
				Scheme:     combinedScheme,
				Repository: mockRepo,
			}

			spec := &v1alpha1.AddOCIArtifact{
				Type: runtime.NewVersionedType(v1alpha1.AddOCIArtifactType, v1alpha1.AddOCIArtifactVersion),
				Spec: tt.spec,
			}

			result, err := transformer.Transform(ctx, spec)
			assert.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}
