package input_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/helm/input"
	v1 "ocm.software/open-component-model/bindings/go/helm/input/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestInputMethod_GetResourceCredentialConsumerIdentity(t *testing.T) {
	inputMethod := &input.InputMethod{}

	tests := []struct {
		name           string
		helmSpec       v1.Helm
		expectError    bool
		expectIdentity bool
	}{
		{
			name: "local helm chart - no credentials needed",
			helmSpec: v1.Helm{
				Type: runtime.Type{
					Name: v1.Type,
				},
				Path: "/path/to/local/chart",
			},
			expectError:    true, // Should return ErrLocalHelmInputDoesNotRequireCredentials
			expectIdentity: false,
		},
		{
			name: "remote helm repository - credentials may be needed",
			helmSpec: v1.Helm{
				Type: runtime.Type{
					Name: v1.Type,
				},
				HelmRepository: "https://charts.example.com",
			},
			expectError:    false,
			expectIdentity: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := &constructorruntime.Resource{
				AccessOrInput: constructorruntime.AccessOrInput{
					Input: &tt.helmSpec,
				},
			}

			identity, err := inputMethod.GetResourceCredentialConsumerIdentity(t.Context(), resource)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, identity)
			} else {
				assert.NoError(t, err)
				if tt.expectIdentity {
					assert.NotNil(t, identity)
					assert.Equal(t, "helm", identity["type"])
					assert.Equal(t, tt.helmSpec.HelmRepository, identity["repository"])
				}
			}
		})
	}
}

func TestInputMethod_ProcessResource_LocalChart(t *testing.T) {
	testDataDir := filepath.Join("testdata", "mychart")
	helmSpec := v1.Helm{
		Type: runtime.Type{
			Name: v1.Type,
		},
		Path: testDataDir,
	}

	resource := &constructorruntime.Resource{
		AccessOrInput: constructorruntime.AccessOrInput{
			Input: &helmSpec,
		},
	}

	inputMethod := &input.InputMethod{}
	result, err := inputMethod.ProcessResource(t.Context(), resource, nil)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, result.ProcessedBlobData, "should have blob data for local chart")
	assert.Nil(t, result.ProcessedResource, "should not have remote resource for local chart")
}

func TestInputMethod_ProcessResource_RemoteChart_Podinfo_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	helmSpec := v1.Helm{
		Type: runtime.Type{
			Name: v1.Type,
		},
		// TODO: Only direct URLs to chart tgz files are supported currently.
		// Need to add support for index.yaml based repositories maybe?
		// "https://stefanprodan.github.io/podinfo/" is supposed to be the index URL or rather the repository.
		// For now, using direct link to a specific chart version.
		HelmRepository: "https://stefanprodan.github.io/podinfo/podinfo-6.9.1.tgz",
		Version:        "6.9.1",
	}

	resource := &constructorruntime.Resource{
		AccessOrInput: constructorruntime.AccessOrInput{
			Input: &helmSpec,
		},
	}

	inputMethod := &input.InputMethod{}

	result, err := inputMethod.ProcessResource(t.Context(), resource, nil)

	require.NoError(t, err, "should successfully download podinfo chart")
	assert.NotNil(t, result, "result should not be nil")
	assert.NotNil(t, result.ProcessedBlobData, "should have blob data for remote chart")
	assert.NotNil(t, result.ProcessedResource, "should have remote resource access info")

	// Verify the remote resource structure
	assert.Equal(t, "podinfo-6.9.1.tgz", result.ProcessedResource.Name, "chart name should be extracted correctly")
	assert.Equal(t, "6.9.1", result.ProcessedResource.Version, "version should match specification")
	assert.Equal(t, input.HelmRepositoryType, result.ProcessedResource.Type, "resource type should be helmRepository")

	// Verify blob data is not empty by reading some content
	reader, err := result.ProcessedBlobData.ReadCloser()
	require.NoError(t, err)
	defer reader.Close()

	// Read first few bytes to verify content exists
	buffer := make([]byte, 100)
	n, err := reader.Read(buffer)
	require.NoError(t, err)
	assert.Greater(t, n, 0, "blob should contain data")
}

func TestInputMethod_ProcessResource_RemoteChart_PodinfoLatest_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Test with latest version URL
	helmSpec := v1.Helm{
		Type: runtime.Type{
			Name: v1.Type,
		},
		HelmRepository: "https://stefanprodan.github.io/podinfo/podinfo-6.9.1.tgz", // Use latest available version
		Version:        "6.9.1",
	}

	resource := &constructorruntime.Resource{
		AccessOrInput: constructorruntime.AccessOrInput{
			Input: &helmSpec,
		},
	}

	inputMethod := &input.InputMethod{}

	result, err := inputMethod.ProcessResource(t.Context(), resource, nil)

	require.NoError(t, err, "should successfully download latest podinfo chart")
	assert.NotNil(t, result, "result should not be nil")
	assert.NotNil(t, result.ProcessedBlobData, "should have blob data for remote chart")
	assert.NotNil(t, result.ProcessedResource, "should have remote resource access info")

	// Verify the remote resource structure
	assert.Equal(t, "podinfo-6.9.1.tgz", result.ProcessedResource.Name, "chart name should be extracted correctly")
	assert.Equal(t, input.HelmRepositoryType, result.ProcessedResource.Type, "resource type should be helmRepository")

	// Verify blob data is not empty by reading some content
	reader, err := result.ProcessedBlobData.ReadCloser()
	require.NoError(t, err)
	defer reader.Close()

	// Read first few bytes to verify content exists
	buffer := make([]byte, 100)
	n, err := reader.Read(buffer)
	require.NoError(t, err)
	assert.Greater(t, n, 0, "blob should contain data")
}

func TestInputMethod_ProcessResource_BothPathAndRepo(t *testing.T) {
	ctx := context.Background()

	testDataDir := filepath.Join("testdata", "mychart")

	helmSpec := v1.Helm{
		Type: runtime.Type{
			Name: v1.Type,
		},
		Path:           testDataDir,
		HelmRepository: "https://charts.example.com",
	}

	resource := &constructorruntime.Resource{
		AccessOrInput: constructorruntime.AccessOrInput{
			Input: &helmSpec,
		},
	}

	inputMethod := &input.InputMethod{}
	_, err := inputMethod.ProcessResource(ctx, resource, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "only one of path or helmRepository can be specified")
}
