package input_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/helm/input"
	helminputspec "ocm.software/open-component-model/bindings/go/helm/spec/input"
	v1 "ocm.software/open-component-model/bindings/go/helm/spec/input/v1"
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
			expectError:    false, // Local inputs return nil, nil — no credentials needed
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
				require.Error(t, err)
				assert.Nil(t, identity)
			} else {
				assert.NoError(t, err)
				if tt.expectIdentity {
					assert.NotNil(t, identity)
					assert.Equal(t, input.LegacyHelmChartConsumerType, identity["type"])
					assert.Equal(t, "https", identity["scheme"])

					// Check hostname based on the specific test case
					switch tt.helmSpec.HelmRepository {
					case "https://charts.example.com":
						assert.Equal(t, "charts.example.com", identity["hostname"])
						assert.Empty(t, identity["port"]) // No port specified
						assert.Empty(t, identity["path"]) // No path specified
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

func TestInputMethodGetResourceCredentialConsumerIdentityOCI(t *testing.T) {
	inputMethod := &input.InputMethod{}

	t.Run("returns OCIRegistry type for oci:// scheme", func(t *testing.T) {
		resource := &constructorruntime.Resource{
			AccessOrInput: constructorruntime.AccessOrInput{
				Input: &v1.Helm{
					Type: runtime.Type{
						Name: v1.Type,
					},
					HelmRepository: "oci://ghcr.io/stefanprodan/charts/podinfo:6.9.1",
				},
			},
		}

		identity, err := inputMethod.GetResourceCredentialConsumerIdentity(t.Context(), resource)
		require.NoError(t, err)
		require.NotNil(t, identity)

		assert.Equal(t, "OCIRegistry", identity["type"])
		assert.Equal(t, "oci", identity["scheme"])
		assert.Equal(t, "ghcr.io", identity["hostname"])
	})

	t.Run("returns HelmChartRepository type for https:// scheme", func(t *testing.T) {
		resource := &constructorruntime.Resource{
			AccessOrInput: constructorruntime.AccessOrInput{
				Input: &v1.Helm{
					Type: runtime.Type{
						Name: v1.Type,
					},
					HelmRepository: "https://charts.example.com/stable",
				},
			},
		}

		identity, err := inputMethod.GetResourceCredentialConsumerIdentity(t.Context(), resource)
		require.NoError(t, err)
		require.NotNil(t, identity)

		assert.Equal(t, "HelmChartRepository", identity["type"])
		assert.Equal(t, "https", identity["scheme"])
		assert.Equal(t, "charts.example.com", identity["hostname"])
	})
}

func TestInputMethodProcessResourceLocalChart(t *testing.T) {
	testDataDir := filepath.Join("../testdata", "mychart")
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

func TestInputMethodProcessResourceBothPathAndRepo(t *testing.T) {
	ctx := context.Background()

	testDataDir := filepath.Join("../testdata", "mychart")

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

func TestScheme_ResolvesUpperCamelCase_Helm(t *testing.T) {
	obj, err := helminputspec.Scheme.NewObject(runtime.NewVersionedType(v1.Type, v1.Version))
	require.NoError(t, err, "Scheme must resolve UpperCamelCase type Helm/v1")
	require.IsType(t, &v1.Helm{}, obj, "expected *v1.Helm from Scheme")
}

func TestScheme_ResolvesLegacy_Helm(t *testing.T) {
	obj, err := helminputspec.Scheme.NewObject(runtime.NewVersionedType(v1.LegacyType, v1.Version))
	require.NoError(t, err, "Scheme must resolve legacy type helm/v1")
	require.IsType(t, &v1.Helm{}, obj, "expected *v1.Helm from Scheme for legacy type")
}
