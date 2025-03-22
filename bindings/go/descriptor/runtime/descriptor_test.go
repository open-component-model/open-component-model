package runtime_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
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
	d := runtime.Descriptor{
		Meta: runtime.Meta{Version: "v1"},
		Component: runtime.Component{
			ComponentMeta: runtime.ComponentMeta{
				ObjectMeta: runtime.ObjectMeta{
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
	c := runtime.Component{
		ComponentMeta: runtime.ComponentMeta{
			ObjectMeta: runtime.ObjectMeta{
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

	descriptor, err := runtime.ConvertFromV2(&v2Descriptor)
	require.NoError(t, err)

	assert.Equal(t, "github.com/weaveworks/weave-gitops", descriptor.Component.Name)
	assert.Equal(t, "v1.0.0", descriptor.Component.Version)
	assert.Equal(t, "weaveworks", descriptor.Component.Provider[v2.IdentityAttributeName])
}

func TestConvertToV2(t *testing.T) {
	var v2Descriptor v2.Descriptor
	err := json.Unmarshal([]byte(jsonData), &v2Descriptor)
	require.NoError(t, err)

	descriptor, err := runtime.ConvertFromV2(&v2Descriptor)
	require.NoError(t, err)

	convertedV2Descriptor, err := runtime.ConvertToV2(descriptor)
	require.NoError(t, err)

	assert.Equal(t, v2Descriptor, *convertedV2Descriptor)
	assert.Empty(t, convertedV2Descriptor.Component.Resources[0].Labels)
	assert.Empty(t, convertedV2Descriptor.Component.Resources[0].SourceRefs)
	assert.Empty(t, convertedV2Descriptor.Component.Sources)
	assert.Empty(t, convertedV2Descriptor.Component.References)
	assert.Empty(t, convertedV2Descriptor.Signatures)
}
