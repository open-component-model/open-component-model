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

func TestInputMethodGetResourceCredentialConsumerIdentity(t *testing.T) {
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
		{
			name: "remote helm repository with port and path - credentials may be needed",
			helmSpec: v1.Helm{
				Type: runtime.Type{
					Name: v1.Type,
				},
				HelmRepository: "https://registry.example.com:8443/helm/charts/myapp-1.0.0.tgz",
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
					assert.Equal(t, "helm/v1", identity["type"])
					assert.Equal(t, "https", identity["scheme"])

					// Check hostname based on the specific test case
					switch tt.helmSpec.HelmRepository {
					case "https://charts.example.com":
						assert.Equal(t, "charts.example.com", identity["hostname"])
						assert.Equal(t, "", identity["port"]) // No port specified
						assert.Equal(t, "", identity["path"]) // No path specified
					case "https://registry.example.com:8443/helm/charts/myapp-1.0.0.tgz":
						assert.Equal(t, "registry.example.com", identity["hostname"])
						assert.Equal(t, "8443", identity["port"])
						assert.Equal(t, "helm/charts/myapp-1.0.0.tgz", identity["path"])
					}
				}
			}
		})
	}
}

func TestInputMethodProcessResourceLocalChart(t *testing.T) {
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

func TestInputMethodProcessResourceRemoteChartPodinfoIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	testCases := []struct {
		name     string
		resource *constructorruntime.Resource
	}{
		{
			name: "remote chart with version https",
			resource: &constructorruntime.Resource{
				AccessOrInput: constructorruntime.AccessOrInput{
					Input: &v1.Helm{
						Type: runtime.Type{
							Name: v1.Type,
						},
						Repository:     "https://internal.charts.example.com/charts",
						HelmRepository: "https://stefanprodan.github.io/podinfo/podinfo-6.9.1.tgz",
						Version:        "6.9.1",
					},
				},
			},
		},
		{
			name: "remote chart with version oci",
			resource: &constructorruntime.Resource{
				AccessOrInput: constructorruntime.AccessOrInput{
					Input: &v1.Helm{
						Type: runtime.Type{
							Name: v1.Type,
						},
						Repository:     "https://internal.charts.example.com/charts",
						HelmRepository: "oci://ghcr.io/stefanprodan/charts/podinfo",
						Version:        "6.9.1",
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			inputMethod := &input.InputMethod{}

			result, err := inputMethod.ProcessResource(t.Context(), tc.resource, nil)
			require.NoError(t, err, "should successfully download podinfo chart")
			assert.NotNil(t, result, "result should not be nil")
			assert.NotNil(t, result.ProcessedBlobData, "should have blob data for remote chart")
			assert.NotNil(t, result.ProcessedResource, "should have remote resource access info")

			// Verify the remote resource structure
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
		})
	}
}

func TestInputMethodProcessResourceBothPathAndRepo(t *testing.T) {
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
