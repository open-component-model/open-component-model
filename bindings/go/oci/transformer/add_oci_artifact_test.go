package transformer

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestAddOCIArtifact_Transform(t *testing.T) {
	ctx := context.Background()
	scheme := oci.DefaultRepositoryScheme

	t.Run("uploads OCI artifact successfully", func(t *testing.T) {
		// Create test blob content
		testContent := []byte("test oci artifact content")
		testBlob := inmemory.New(bytes.NewReader(testContent))

		// Create transformation spec
		transformation := &v1alpha1.AddOCIArtifact{
			Type: runtime.NewVersionedType(v1alpha1.AddOCIArtifactType, v1alpha1.Version),
			ID:   "test-add",
			Spec: &v1alpha1.AddOCIArtifactSpec{
				OCIArtifact:    testBlob,
				TargetRegistry: "ghcr.io/target/artifact:v1.0.0",
			},
		}

		// Create mock repository (for uploading)
		mockRepo := &mockResourceRepositoryForAdd{
			uploadFunc: func(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob, creds map[string]string) (*descriptor.Resource, error) {
				assert.NotNil(t, content)
				// Return resource with digest
				res = res.DeepCopy()
				res.Digest = &descriptor.Digest{
					HashAlgorithm:          "SHA-256",
					NormalisationAlgorithm: "ociArtifactDigest/v1",
					Value:                  "abcd1234",
				}
				return res, nil
			},
		}

		// Create transformer
		tr := &AddOCIArtifact{
			Scheme:             scheme,
			Repository:         mockRepo,
			CredentialProvider: nil,
		}

		// Execute transformation
		result, err := tr.Transform(ctx, transformation)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Verify output
		resultTransform, ok := result.(*v1alpha1.AddOCIArtifact)
		require.True(t, ok)
		require.NotNil(t, resultTransform.Output)
		require.NotNil(t, resultTransform.Output.Resource)
		require.NotNil(t, resultTransform.Output.Resource.Digest)
		assert.Equal(t, "abcd1234", resultTransform.Output.Resource.Digest.Value)
	})

	t.Run("returns error when spec is nil", func(t *testing.T) {
		transformation := &v1alpha1.AddOCIArtifact{
			Type: runtime.NewVersionedType(v1alpha1.AddOCIArtifactType, v1alpha1.Version),
			ID:   "test-add",
			Spec: nil,
		}

		tr := &AddOCIArtifact{
			Scheme:     scheme,
			Repository: &mockResourceRepositoryForAdd{},
		}

		_, err := tr.Transform(ctx, transformation)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "spec is required")
	})

	t.Run("returns error when ociArtifact blob is nil", func(t *testing.T) {
		transformation := &v1alpha1.AddOCIArtifact{
			Type: runtime.NewVersionedType(v1alpha1.AddOCIArtifactType, v1alpha1.Version),
			ID:   "test-add",
			Spec: &v1alpha1.AddOCIArtifactSpec{
				OCIArtifact:    nil,
				TargetRegistry: "ghcr.io/target/artifact:v1.0.0",
			},
		}

		tr := &AddOCIArtifact{
			Scheme:     scheme,
			Repository: &mockResourceRepositoryForAdd{},
		}

		_, err := tr.Transform(ctx, transformation)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ociArtifact blob is required")
	})

	t.Run("returns error when targetRef is empty", func(t *testing.T) {
		testBlob := inmemory.New(bytes.NewReader([]byte("test content")))

		transformation := &v1alpha1.AddOCIArtifact{
			Type: runtime.NewVersionedType(v1alpha1.AddOCIArtifactType, v1alpha1.Version),
			ID:   "test-add",
			Spec: &v1alpha1.AddOCIArtifactSpec{
				OCIArtifact:    testBlob,
				TargetRegistry: "",
			},
		}

		tr := &AddOCIArtifact{
			Scheme:     scheme,
			Repository: &mockResourceRepositoryForAdd{},
		}

		_, err := tr.Transform(ctx, transformation)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "targetRef is required")
	})

	t.Run("handles upload errors gracefully", func(t *testing.T) {
		testBlob := inmemory.New(bytes.NewReader([]byte("test content")))

		transformation := &v1alpha1.AddOCIArtifact{
			Type: runtime.NewVersionedType(v1alpha1.AddOCIArtifactType, v1alpha1.Version),
			ID:   "test-add",
			Spec: &v1alpha1.AddOCIArtifactSpec{
				OCIArtifact:    testBlob,
				TargetRegistry: "ghcr.io/target/artifact:v1.0.0",
			},
		}

		mockRepo := &mockResourceRepositoryForAdd{
			uploadFunc: func(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob, creds map[string]string) (*descriptor.Resource, error) {
				return nil, assert.AnError
			},
		}

		tr := &AddOCIArtifact{
			Scheme:     scheme,
			Repository: mockRepo,
		}

		_, err := tr.Transform(ctx, transformation)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed uploading OCI artifact")
	})
}

// mockResourceRepositoryForAdd is a mock implementation of repository.ResourceRepository for testing AddOCIArtifact
type mockResourceRepositoryForAdd struct {
	uploadFunc                        func(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob, creds map[string]string) (*descriptor.Resource, error)
	getCredentialConsumerIdentityFunc func(ctx context.Context, res *descriptor.Resource) (runtime.Identity, error)
}

func (m *mockResourceRepositoryForAdd) UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob, creds map[string]string) (*descriptor.Resource, error) {
	if m.uploadFunc != nil {
		return m.uploadFunc(ctx, res, content, creds)
	}
	return res, nil
}

func (m *mockResourceRepositoryForAdd) DownloadResource(ctx context.Context, res *descriptor.Resource, creds map[string]string) (blob.ReadOnlyBlob, error) {
	return nil, nil
}

func (m *mockResourceRepositoryForAdd) GetResourceCredentialConsumerIdentity(ctx context.Context, res *descriptor.Resource) (runtime.Identity, error) {
	if m.getCredentialConsumerIdentityFunc != nil {
		return m.getCredentialConsumerIdentityFunc(ctx, res)
	}
	return nil, nil
}
