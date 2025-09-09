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
	ctx := context.Background()
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

			identity, err := inputMethod.GetResourceCredentialConsumerIdentity(ctx, resource)

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
	ctx := context.Background()

	// Get test data directory
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
	result, err := inputMethod.ProcessResource(ctx, resource, nil)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, result.ProcessedBlobData, "should have blob data for local chart")
	assert.Nil(t, result.ProcessedResource, "should not have remote resource for local chart")
}

func TestInputMethod_ProcessResource_RemoteChart_Structure(t *testing.T) {
	ctx := context.Background()

	// TODO: either figure something out or make this an integration test?
	helmSpec := v1.Helm{
		Type: runtime.Type{
			Name: v1.Type,
		},
		HelmRepository: "https://charts.example.com",
		Version:        "1.0.0",
		Path:           "nginx",
	}

	resource := &constructorruntime.Resource{
		AccessOrInput: constructorruntime.AccessOrInput{
			Input: &helmSpec,
		},
	}

	inputMethod := &input.InputMethod{}

	result, err := inputMethod.ProcessResource(ctx, resource, map[string]string{
		"username": "testuser",
		"password": "testpass",
	})

	// TODO: update this.
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestInputMethod_ProcessResource_BothPathAndRepo(t *testing.T) {
	ctx := context.Background()

	testDataDir := filepath.Join("testdata", "mychart")

	// When both path and repository are provided, path should take precedence
	helmSpec := v1.Helm{
		Type: runtime.Type{
			Name: v1.Type,
		},
		Path:           testDataDir,                  // Local path (should take precedence)
		HelmRepository: "https://charts.example.com", // Remote repo (should be ignored)
	}

	resource := &constructorruntime.Resource{
		AccessOrInput: constructorruntime.AccessOrInput{
			Input: &helmSpec,
		},
	}

	inputMethod := &input.InputMethod{}
	_, err := inputMethod.ProcessResource(ctx, resource, nil)

	require.Error(t, err)
}
