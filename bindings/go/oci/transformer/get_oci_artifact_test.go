package transformer

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	filesystemaccess "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// mockRepositoryForGetOCI implements ResourceRepository for testing GetOCIArtifact
type mockRepositoryForGetOCI struct {
	returnBlob blob.ReadOnlyBlob
}

func (m mockRepositoryForGetOCI) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	return nil, nil
}

func (m mockRepositoryForGetOCI) UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob, credentials map[string]string) (*descriptor.Resource, error) {
	return nil, nil
}

func (m mockRepositoryForGetOCI) DownloadResource(ctx context.Context, res *descriptor.Resource, credentials map[string]string) (blob.ReadOnlyBlob, error) {
	return m.returnBlob, nil
}

func TestGetOCIArtifact_Transform_OCI(t *testing.T) {
	ctx := t.Context()

	// Setup test data - create a blob that the repository will return (OCI artifact as tar)
	testBlobData := []byte("test oci artifact content as tar archive")
	testBlob := inmemory.New(bytes.NewReader(testBlobData))
	testBlob.SetMediaType("application/vnd.ocm.software.oci.layout.v1+tar+gzip")

	mockRepo := &mockRepositoryForGetOCI{
		returnBlob: testBlob,
	}

	// Create a combined scheme
	combinedScheme := runtime.NewScheme()
	v2.MustAddToScheme(combinedScheme)
	filesystemaccess.MustAddToScheme(combinedScheme)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.GetOCIArtifact{}, v1alpha1.GetOCIArtifactV1alpha1)

	transformer := &GetOCIArtifact{
		Scheme:     combinedScheme,
		Repository: mockRepo,
	}

	// Create transformation spec
	spec := &v1alpha1.GetOCIArtifact{
		Type: runtime.NewVersionedType(v1alpha1.GetOCIArtifactType, v1alpha1.Version),
		ID:   "test-get-oci-transform",
		Spec: &v1alpha1.GetOCIArtifactSpec{
			Resource: &v2.Resource{
				ElementMeta: v2.ElementMeta{
					ObjectMeta: v2.ObjectMeta{
						Name:    "test-image",
						Version: "1.21.0",
					},
				},
				Type:     "ociImage",
				Relation: "external",
				Access: &runtime.Raw{
					Type: runtime.Type{
						Name:    "ociArtifact",
						Version: "v1",
					},
					Data: []byte(`{ "imageReference": "ghcr.io/open-component-model/helmexample/charts/mariadb:12.2.7" }`),
				},
			},
		},
	}

	// Execute transformation
	result, err := transformer.Transform(ctx, spec)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify result
	transformed, ok := result.(*v1alpha1.GetOCIArtifact)
	require.True(t, ok)
	require.NotNil(t, transformed.Output)
	require.NotNil(t, transformed.Output.Resource)

	// Verify file was created
	osPath := strings.ReplaceAll(transformed.Output.File.URI, "file://", "")
	assert.FileExists(t, strings.ReplaceAll(osPath, "file://", ""))

	// Verify file content
	fileContent, err := os.ReadFile(osPath)
	require.NoError(t, err)
	assert.Equal(t, testBlobData, fileContent)

	// Verify resource in output
	assert.Equal(t, "test-image", transformed.Output.Resource.Name)
	assert.Equal(t, "1.21.0", transformed.Output.Resource.Version)
}

func TestGetOCIArtifact_Transform_OCI_WithOutputPath(t *testing.T) {
	ctx := context.Background()

	// Setup test data - create a blob that the repository will return (OCI artifact as tar)
	testBlobData := []byte("test oci artifact content as tar archive")
	testBlob := inmemory.New(bytes.NewReader(testBlobData))
	testBlob.SetMediaType("application/vnd.ocm.software.oci.layout.v1+tar+gzip")

	mockRepo := &mockRepositoryForGetOCI{
		returnBlob: testBlob,
	}

	// Create a combined scheme
	combinedScheme := runtime.NewScheme()
	v2.MustAddToScheme(combinedScheme)
	filesystemaccess.MustAddToScheme(combinedScheme)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.GetOCIArtifact{}, v1alpha1.GetOCIArtifactV1alpha1)

	transformer := &GetOCIArtifact{
		Scheme:     combinedScheme,
		Repository: mockRepo,
	}

	// Create temporary directory for output
	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "oci-artifact.tar.gz")

	// Create transformation spec
	spec := &v1alpha1.GetOCIArtifact{
		Type: runtime.NewVersionedType(v1alpha1.GetOCIArtifactType, v1alpha1.Version),
		ID:   "test-get-oci-transform",
		Spec: &v1alpha1.GetOCIArtifactSpec{
			Resource: &v2.Resource{
				ElementMeta: v2.ElementMeta{
					ObjectMeta: v2.ObjectMeta{
						Name:    "test-image",
						Version: "1.21.0",
					},
				},
				Type:     "ociImage",
				Relation: "external",
				Access: &runtime.Raw{
					Type: runtime.Type{
						Name:    "ociArtifact",
						Version: "v1",
					},
					Data: []byte(`{ "imageReference": "ghcr.io/open-component-model/helmexample/charts/mariadb:12.2.7" }`),
				},
			},
			OutputPath: outputPath,
		},
	}

	// Execute transformation
	result, err := transformer.Transform(ctx, spec)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify result
	transformed, ok := result.(*v1alpha1.GetOCIArtifact)
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
	assert.Equal(t, "test-image", transformed.Output.Resource.Name)
	assert.Equal(t, "1.21.0", transformed.Output.Resource.Version)
}

func TestGetOCIArtifact_Transform_OCI_Should_Default_No_Ext(t *testing.T) {
	ctx := context.Background()

	// Setup test data - create a blob that the repository will return (OCI artifact as tar)
	testBlobData := []byte("test oci artifact content as tar archive")
	testBlob := inmemory.New(bytes.NewReader(testBlobData))
	testBlob.SetMediaType("application/vnd.ocm.software.oci.image.v1+tar+gzip")

	mockRepo := &mockRepositoryForGetOCI{
		returnBlob: testBlob,
	}

	// Create a combined scheme
	combinedScheme := runtime.NewScheme()
	v2.MustAddToScheme(combinedScheme)
	filesystemaccess.MustAddToScheme(combinedScheme)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.GetOCIArtifact{}, v1alpha1.GetOCIArtifactV1alpha1)

	transformer := &GetOCIArtifact{
		Scheme:     combinedScheme,
		Repository: mockRepo,
	}

	// Create transformation spec
	spec := &v1alpha1.GetOCIArtifact{
		Type: runtime.NewVersionedType(v1alpha1.GetOCIArtifactType, v1alpha1.Version),
		ID:   "test-get-oci-transform",
		Spec: &v1alpha1.GetOCIArtifactSpec{
			Resource: &v2.Resource{
				ElementMeta: v2.ElementMeta{
					ObjectMeta: v2.ObjectMeta{
						Name:    "test-image",
						Version: "1.21.0",
					},
				},
				Type:     "ociImage",
				Relation: "external",
				Access: &runtime.Raw{
					Type: runtime.Type{
						Name:    "ociArtifact",
						Version: "v1",
					},
					Data: []byte(`{ "imageReference": "ghcr.io/open-component-model/helmexample/charts/mariadb:12.2.7" }`),
				},
			},
		},
	}

	// Execute transformation
	result, err := transformer.Transform(ctx, spec)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify result
	transformed, ok := result.(*v1alpha1.GetOCIArtifact)
	require.True(t, ok)
	require.NotNil(t, transformed.Output)
	require.NotNil(t, transformed.Output.Resource)

	// Verify file was created
	osPath := strings.ReplaceAll(transformed.Output.File.URI, "file://", "")
	assert.FileExists(t, strings.ReplaceAll(osPath, "file://", ""))

	// Verify file content
	fileContent, err := os.ReadFile(osPath)
	require.NoError(t, err)
	assert.Equal(t, testBlobData, fileContent)

	// Verify resource in output
	assert.Equal(t, "test-image", transformed.Output.Resource.Name)
	assert.Equal(t, "1.21.0", transformed.Output.Resource.Version)
}

func TestGetOCIArtifact_Transform_ValidationErrors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		spec        *v1alpha1.GetOCIArtifactSpec
		expectedErr string
	}{
		{
			name: "missing resource",
			spec: &v1alpha1.GetOCIArtifactSpec{
				Resource: nil,
			},
			expectedErr: "resource is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &mockRepositoryForGetOCI{}

			combinedScheme := runtime.NewScheme()
			v2.MustAddToScheme(combinedScheme)
			filesystemaccess.MustAddToScheme(combinedScheme)
			combinedScheme.MustRegisterWithAlias(&v1alpha1.GetOCIArtifact{}, v1alpha1.GetOCIArtifactV1alpha1)

			transformer := &GetOCIArtifact{
				Scheme:     combinedScheme,
				Repository: mockRepo,
			}

			spec := &v1alpha1.GetOCIArtifact{
				Type: runtime.NewVersionedType(v1alpha1.GetOCIArtifactType, v1alpha1.Version),
				Spec: tt.spec,
			}

			result, err := transformer.Transform(ctx, spec)
			assert.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}
