package runtime_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptorRuntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const jsonData = `
{
  "meta": {
    "schemaVersion": "v2"
  },
  "component": {
    "name": "github.com/weaveworks/weave-gitops",
    "version": "v1.0.0",
    "provider": "weaveworks",
    "labels": [
      {
        "name": "link-to-documentation",
        "value": "https://github.com/weaveworks/weave-gitops"
      }
    ],
    "repositoryContexts": [
      {
        "baseUrl": "ghcr.io",
        "componentNameMapping": "urlPath",
        "subPath": "phoban01/ocm",
        "type": "OCIRegistry"
      }
    ],
    "resources": [
      {
        "name": "image",
        "relation": "external",
        "type": "ociImage",
        "version": "v0.14.1",
        "access": {
          "type": "ociArtifact",
          "imageReference": "ghcr.io/weaveworks/wego-app:v0.14.1"
        },
        "digest": {
          "hashAlgorithm": "SHA-256",
          "value": "abc123"
        }
      }
    ]
  }
}`

func TestDescriptorString(t *testing.T) {
	d := descriptorRuntime.Descriptor{
		Meta: descriptorRuntime.Meta{Version: "v1"},
		Component: descriptorRuntime.Component{
			ComponentMeta: descriptorRuntime.ComponentMeta{
				ObjectMeta: descriptorRuntime.ObjectMeta{
					Name:    "test-component",
					Version: "1.0.0",
				},
			},
		},
	}

	expected := "test-component:1.0.0 (schema version v1)"
	if d.String() != expected {
		t.Errorf("expected %s, got %s", expected, d.String())
	}
}

func TestComponentString(t *testing.T) {
	c := descriptorRuntime.Component{
		ComponentMeta: descriptorRuntime.ComponentMeta{
			ObjectMeta: descriptorRuntime.ObjectMeta{
				Name:    "test-component",
				Version: "1.0.0",
			},
		},
	}

	expected := "test-component:1.0.0"
	if c.String() != expected {
		t.Errorf("expected %s, got %s", expected, c.String())
	}
}

func TestConvertFromV2(t *testing.T) {
	var v2Descriptor v2.Descriptor
	err := json.Unmarshal([]byte(jsonData), &v2Descriptor)
	require.NoError(t, err)

	descriptor, err := descriptorRuntime.ConvertFromV2(&v2Descriptor)
	require.NoError(t, err)

	assert.Equal(t, "github.com/weaveworks/weave-gitops", descriptor.Component.Name)
	assert.Equal(t, "v1.0.0", descriptor.Component.Version)
	assert.Equal(t, "weaveworks", descriptor.Component.Provider[v2.IdentityAttributeName])
}

func TestConvertToV2(t *testing.T) {
	var v2Descriptor v2.Descriptor
	err := json.Unmarshal([]byte(jsonData), &v2Descriptor)
	require.NoError(t, err)

	descriptor, err := descriptorRuntime.ConvertFromV2(&v2Descriptor)
	require.NoError(t, err)

	scheme := runtime.NewScheme()

	convertedV2Descriptor, err := descriptorRuntime.ConvertToV2(scheme, descriptor)
	require.NoError(t, err)

	assert.Equal(t, v2Descriptor, *convertedV2Descriptor)
	assert.NotEmpty(t, convertedV2Descriptor.Component.Resources[0].Name)
	assert.NotEmpty(t, convertedV2Descriptor.Component.Resources[0].Access.Data)
	assert.Empty(t, convertedV2Descriptor.Component.Sources)
	assert.Empty(t, convertedV2Descriptor.Component.References)
	assert.Empty(t, convertedV2Descriptor.Signatures)
}
