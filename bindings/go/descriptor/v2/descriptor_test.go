package descriptor_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/descriptor"
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
          "normalisationAlgorithm": "ociArtifactDigest/v1",
          "value": "efa2b9980ca2de65dc5a0c8cc05638b1a4b4ce8f6972dc08d0e805e5563ba5bb"
        }
      }
    ],
    "sources": [
      {
        "name": "weave-gitops",
        "type": "git",
        "version": "v0.14.1",
        "access": {
          "commit": "727513969553bfcc603e1c0ae1a75d79e4132b58",
          "ref": "refs/tags/v0.14.1",
          "repoUrl": "github.com/weaveworks/weave-gitops",
          "type": "gitHub"
        }
      }
    ],
    "componentReferences": [
      {
        "name": "prometheus",
        "version": "v1.0.0",
        "componentName": "cncf.io/prometheus",
        "digest": {
          "hashAlgorithm": "SHA-256",
          "normalisationAlgorithm": "jsonNormalisation/v1",
          "value": "04eb20b6fd942860325caf7f4415d1acf287a1aabd9e4827719328ba25d6f801"
        }
      }
    ]
  },
  "signatures": [
    {
      "name": "ww-dev",
      "digest": {
        "hashAlgorithm": "SHA-256",
        "normalisationAlgorithm": "jsonNormalisation/v1",
        "value": "4faff7822616305ecd09284d7c3e74a64f2269dcc524a9cdf0db4b592b8cee6a"
      },
      "signature": {
        "algorithm": "RSASSA-PSS",
        "mediaType": "application/vnd.ocm.signature.rsa",
        "value": "26468587671bdbd2166cf5f69829f090c10768511b15e804294fcb26e552654316c8f4851ed396f279ec99335e5f4b11cb043feb97f1f9a42115f4fda2d31ae8b481b7303b9a913d3a4b92d446fbee9ed487c93b09e513f3f68355040ec08454675e1f407422062abbd2681f70dd5488ad29020b30cfa7e001455c550458da96166bc3243c8426977d73352aface5323fb2b5a374e9c31b272a59c160b85631231c9fc2f23c032401b80fef937029a39111cee34470c61ae86cd4942553466411a5a116159fdcc10e50fe9360c5184028e72d1fe9c7315f26e15d7b4849f62d197501b8cc6b6f1b1391ecc2fc2fc0c1290d2554594505b25fa8f9bfb28c8df24"
      }
    }
  ]
}
`

const yamlData = `
meta:
  schemaVersion: v2
component:
  name: github.com/weaveworks/weave-gitops
  version: v1.0.0
  provider: weaveworks
  labels:
    - name: link-to-documentation
      value: https://github.com/weaveworks/weave-gitops
  repositoryContexts:
    - baseUrl: ghcr.io
      componentNameMapping: urlPath
      subPath: phoban01/ocm
      type: OCIRegistry
  resources:
    - name: image
      relation: external
      type: ociImage
      version: v0.14.1
      access:
        type: ociArtifact
        imageReference: ghcr.io/weaveworks/wego-app:v0.14.1
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: ociArtifactDigest/v1
        value: efa2b9980ca2de65dc5a0c8cc05638b1a4b4ce8f6972dc08d0e805e5563ba5bb
  sources:
    - name: weave-gitops
      type: git
      version: v0.14.1
      access:
        commit: 727513969553bfcc603e1c0ae1a75d79e4132b58
        ref: refs/tags/v0.14.1
        repoUrl: github.com/weaveworks/weave-gitops
        type: gitHub
  componentReferences:
    - name: prometheus
      version: v1.0.0
      componentName: cncf.io/prometheus
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: jsonNormalisation/v1
        value: 04eb20b6fd942860325caf7f4415d1acf287a1aabd9e4827719328ba25d6f801
signatures:
  - name: ww-dev
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: jsonNormalisation/v1
      value: 4faff7822616305ecd09284d7c3e74a64f2269dcc524a9cdf0db4b592b8cee6a
    signature:
      algorithm: RSASSA-PSS
      mediaType: application/vnd.ocm.signature.rsa
      value: 26468587671bdbd2166cf5f69829f090c10768511b15e804294fcb26e552654316c8f4851ed396f279ec99335e5f4b11cb043feb97f1f9a42115f4fda2d31ae8b481b7303b9a913d3a4b92d446fbee9ed487c93b09e513f3f68355040ec08454675e1f407422062abbd2681f70dd5488ad29020b30cfa7e001455c550458da96166bc3243c8426977d73352aface5323fb2b5a374e9c31b272a59c160b85631231c9fc2f23c032401b80fef937029a39111cee34470c61ae86cd4942553466411a5a116159fdcc10e50fe9360c5184028e72d1fe9c7315f26e15d7b4849f62d197501b8cc6b6f1b1391ecc2fc2fc0c1290d2554594505b25fa8f9bfb28c8df24
`

func TestDescriptor_JSON(t *testing.T) {
	desc := descriptor.Descriptor{}
	err := json.Unmarshal([]byte(jsonData), &desc)
	assert.Nil(t, err)

	assert.NotEmpty(t, desc.Component.Resources[0].ToIdentity())

	descData, err := json.Marshal(desc)
	assert.JSONEq(t, jsonData, string(descData))
	assert.Nil(t, err)
}

func TestDescriptor_YAML(t *testing.T) {
	desc := &descriptor.Descriptor{}
	err := yaml.Unmarshal([]byte(yamlData), desc)
	assert.Nil(t, err)
	descData, err := yaml.Marshal(desc)
	assert.YAMLEq(t, yamlData, string(descData))
	assert.Nil(t, err)
	_ = descData
}

func TestDescriptor_String(t *testing.T) {
	// Setup
	desc := descriptor.Descriptor{
		Meta: descriptor.Meta{
			Version: "v1",
		},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-component",
					Version: "1.0.0",
				},
			},
		},
	}

	// Test
	result := desc.String()

	// Assert
	expected := "test-component:1.0.0 (schema version v1)"
	assert.Equal(t, expected, result)
}

func TestComponent_String(t *testing.T) {
	// Setup
	comp := descriptor.Component{
		ComponentMeta: descriptor.ComponentMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-component",
				Version: "1.0.0",
				Labels: []descriptor.Label{
					{Name: "env", Value: "prod"},
				},
			},
		},
	}

	// Test
	result := comp.String()

	// Assert
	expected := "test-component:1.0.0+labels([{env prod false}])"
	assert.Equal(t, expected, result)
}

func TestObjectMeta_String(t *testing.T) {
	tests := []struct {
		name     string
		objMeta  descriptor.ObjectMeta
		expected string
	}{
		{
			name: "with name only",
			objMeta: descriptor.ObjectMeta{
				Name: "test-object",
			},
			expected: "test-object",
		},
		{
			name: "with name and version",
			objMeta: descriptor.ObjectMeta{
				Name:    "test-object",
				Version: "1.0.0",
			},
			expected: "test-object:1.0.0",
		},
		{
			name: "with name, version and labels",
			objMeta: descriptor.ObjectMeta{
				Name:    "test-object",
				Version: "1.0.0",
				Labels: []descriptor.Label{
					{Name: "type", Value: "library"},
					{Name: "priority", Value: "high"},
				},
			},
			expected: "test-object:1.0.0+labels([{type library false} {priority high false}])",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.objMeta.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestElementMeta_String(t *testing.T) {
	// Setup
	elemMeta := descriptor.ElementMeta{
		ObjectMeta: descriptor.ObjectMeta{
			Name:    "test-element",
			Version: "2.0.0",
			Labels: []descriptor.Label{
				{Name: "type", Value: "backend"},
			},
		},
		ExtraIdentity: runtime.Identity{
			"namespace": "system",
			"platform":  "linux",
		},
	}

	// Test
	result := elemMeta.String()

	// Assert
	assert.Contains(t, result, "test-element:2.0.0")
	assert.Contains(t, result, "+labels([{type backend false}])")
	assert.Contains(t, result, "+extraIdentity(map[")
	assert.Contains(t, result, "namespace:system")
	assert.Contains(t, result, "platform:linux")
}

func TestElementMeta_ToIdentity(t *testing.T) {
	// Setup
	elemMeta := descriptor.ElementMeta{
		ObjectMeta: descriptor.ObjectMeta{
			Name:    "test-element",
			Version: "2.0.0",
		},
		ExtraIdentity: runtime.Identity{
			"namespace": "system",
		},
	}

	// Test
	identity := elemMeta.ToIdentity()

	// Assert
	expected := runtime.Identity{
		"name":      "test-element",
		"version":   "2.0.0",
		"namespace": "system",
	}
	assert.Equal(t, expected, identity)
}

func TestElementMeta_ToIdentity_Nil(t *testing.T) {
	// Test
	var elemMeta *descriptor.ElementMeta
	identity := elemMeta.ToIdentity()

	// Assert
	assert.Nil(t, identity)
}

func TestComponentMeta_ToIdentity(t *testing.T) {
	// Setup
	compMeta := descriptor.ComponentMeta{
		ObjectMeta: descriptor.ObjectMeta{
			Name:    "test-component",
			Version: "3.0.0",
			Labels: []descriptor.Label{
				{Name: "stage", Value: "dev"},
			},
		},
	}

	// Test
	identity := compMeta.ToIdentity()

	// Assert
	expected := runtime.Identity{
		"name":    "test-component",
		"version": "3.0.0",
	}
	assert.Equal(t, expected, identity)
}

func TestComponentMeta_ToIdentity_Nil(t *testing.T) {
	// Test
	var compMeta *descriptor.ComponentMeta
	identity := compMeta.ToIdentity()

	// Assert
	assert.Nil(t, identity)
}

func TestNewExcludeFromSignatureDigest(t *testing.T) {
	// Test
	digest := descriptor.NewExcludeFromSignatureDigest()

	// Assert
	assert.Equal(t, descriptor.NoDigest, digest.HashAlgorithm)
	assert.Equal(t, descriptor.ExcludeFromSignature, digest.NormalisationAlgorithm)
	assert.Equal(t, descriptor.NoDigest, digest.Value)
}

func TestTimestamp_MarshalJSON(t *testing.T) {
	// Setup
	ts := &descriptor.Timestamp{}
	timeValue := time.Date(2023, 10, 15, 14, 30, 45, 123456789, time.UTC)
	ts.Time = descriptor.NewTime(timeValue)

	// Test
	data, err := json.Marshal(ts)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, `"2023-10-15T14:30:45Z"`, string(data))
}

func TestTimestamp_UnmarshalJSON(t *testing.T) {
	// Setup
	ts := &descriptor.Timestamp{}
	jsonData := []byte(`"2023-10-15T14:30:45Z"`)

	// Test
	err := json.Unmarshal(jsonData, ts)

	// Assert
	require.NoError(t, err)
	expectedTime := time.Date(2023, 10, 15, 14, 30, 45, 0, time.UTC)
	assert.Equal(t, expectedTime, ts.Time.Time)
}

func TestTimestamp_UnmarshalJSON_Null(t *testing.T) {
	// Setup
	ts := &descriptor.Timestamp{}
	jsonData := []byte(`null`)

	// Test
	err := json.Unmarshal(jsonData, ts)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, time.Time{}, ts.Time.Time)
}

func TestTimestamp_UnmarshalJSON_InvalidFormat(t *testing.T) {
	// Setup
	ts := &descriptor.Timestamp{}
	jsonData := []byte(`"2023-10-15"`)

	// Test
	err := json.Unmarshal(jsonData, ts)

	// Assert
	require.Error(t, err)
}

func TestTimestamp_MarshalJSON_InvalidYear(t *testing.T) {
	// Setup
	ts := &descriptor.Timestamp{}
	timeValue := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	ts.Time = descriptor.NewTime(timeValue)

	// Test
	_, err := json.Marshal(ts)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "year outside of range")
}

func TestConstantValues(t *testing.T) {
	// Test identity attribute constants
	assert.Equal(t, "name", descriptor.IdentityAttributeName)
	assert.Equal(t, "version", descriptor.IdentityAttributeVersion)

	// Test digest constants
	assert.Equal(t, "EXCLUDE-FROM-SIGNATURE", descriptor.ExcludeFromSignature)
	assert.Equal(t, "NO-DIGEST", descriptor.NoDigest)

	// Test resource relation constants
	assert.Equal(t, descriptor.ResourceRelation("local"), descriptor.LocalRelation)
	assert.Equal(t, descriptor.ResourceRelation("external"), descriptor.ExternalRelation)
}

func TestResource_Struct(t *testing.T) {
	// Setup
	resource := descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-resource",
				Version: "1.0.0",
			},
		},
		Type:     "ociImage",
		Relation: descriptor.LocalRelation,
		Access: &runtime.Raw{
			Type: runtime.Type{
				Name: "ociArtifact",
			},
			Data: []byte(`{"type":"ociArtifact","imageReference":"test/image:1.0"}`),
		},
		Digest: &descriptor.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: "OciArtifactDigest",
			Value:                  "abcdef1234567890",
		},
		Size: 1024,
	}

	// Test
	jsonData, err := json.Marshal(resource)

	// Assert
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"name":"test-resource"`)
	assert.Contains(t, string(jsonData), `"version":"1.0.0"`)
	assert.Contains(t, string(jsonData), `"type":"ociImage"`)
	assert.Contains(t, string(jsonData), `"relation":"local"`)
	assert.Contains(t, string(jsonData), `"access":{"type":"ociArtifact","imageReference":"test/image:1.0"}`)
	assert.Contains(t, string(jsonData), `"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"OciArtifactDigest","value":"abcdef1234567890"}`)
	assert.Contains(t, string(jsonData), `"size":1024`)
}

func TestSource_Struct(t *testing.T) {
	// Setup
	source := descriptor.Source{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-source",
				Version: "1.0.0",
			},
		},
		Type: "git",
		Access: &runtime.Raw{
			Type: runtime.Type{
				Name: "gitHub",
			},
			Data: []byte(`{"type":"gitHub","repoUrl":"https://github.com/test/repo","commit":"abcdef"}`),
		},
	}

	// Test
	jsonData, err := json.Marshal(source)

	// Assert
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"name":"test-source"`)
	assert.Contains(t, string(jsonData), `"version":"1.0.0"`)
	assert.Contains(t, string(jsonData), `"type":"git"`)
	assert.Contains(t, string(jsonData), `"access":{"type":"gitHub","repoUrl":"https://github.com/test/repo","commit":"abcdef"}`)
}

func TestReference_Struct(t *testing.T) {
	// Setup
	reference := descriptor.Reference{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-reference",
				Version: "1.0.0",
			},
		},
		Component: "referenced-component",
		Digest: descriptor.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: "JsonNormalization",
			Value:                  "0123456789abcdef",
		},
	}

	// Test
	jsonData, err := json.Marshal(reference)

	// Assert
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"name":"test-reference"`)
	assert.Contains(t, string(jsonData), `"version":"1.0.0"`)
	assert.Contains(t, string(jsonData), `"componentName":"referenced-component"`)
	assert.Contains(t, string(jsonData), `"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"JsonNormalization","value":"0123456789abcdef"}`)
}

func TestSignature_Struct(t *testing.T) {
	// Setup
	signature := descriptor.Signature{
		Name: "test-signature",
		Digest: descriptor.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: "JsonNormalization",
			Value:                  "0123456789abcdef",
		},
		Signature: descriptor.SignatureInfo{
			Algorithm: "RSASSA-PKCS1-V1_5",
			Value:     "base64-encoded-signature-value",
			MediaType: "application/vnd.ocm.signature.rsa",
			Issuer:    "Test Issuer",
		},
	}

	// Test
	jsonData, err := json.Marshal(signature)

	// Assert
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"name":"test-signature"`)
	assert.Contains(t, string(jsonData), `"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"JsonNormalization","value":"0123456789abcdef"}`)
	assert.Contains(t, string(jsonData), `"signature":{"algorithm":"RSASSA-PKCS1-V1_5","value":"base64-encoded-signature-value","mediaType":"application/vnd.ocm.signature.rsa","issuer":"Test Issuer"}`)
}

func TestComponentDeserialization(t *testing.T) {
	jsonData := `{
		"meta": {
			"schemaVersion": "v2"
		},
		"component": {
			"name": "example-component",
			"version": "1.0.0",
			"provider": "Example Provider",
			"resources": [
				{
					"name": "example-resource",
					"version": "1.0.0",
					"type": "ociImage",
					"relation": "local",
					"access": {
						"type": "ociArtifact",
						"imageReference": "example/image:1.0.0"
					}
				}
			],
			"sources": [
				{
					"name": "example-source",
					"version": "1.0.0",
					"type": "git",
					"access": {
						"type": "github",
						"repoUrl": "https://github.com/example/repo"
					}
				}
			],
			"componentReferences": [
				{
					"name": "example-reference",
					"version": "1.0.0",
					"componentName": "other-component"
				}
			]
		}
	}`

	var desc descriptor.Descriptor
	err := json.Unmarshal([]byte(jsonData), &desc)

	require.NoError(t, err)
	assert.Equal(t, "v2", desc.Meta.Version)
	assert.Equal(t, "example-component", desc.Component.Name)
	assert.Equal(t, "1.0.0", desc.Component.Version)
	assert.Equal(t, "Example Provider", desc.Component.Provider)

	// Check resources
	require.Len(t, desc.Component.Resources, 1)
	assert.Equal(t, "example-resource", desc.Component.Resources[0].Name)
	assert.Equal(t, "1.0.0", desc.Component.Resources[0].Version)
	assert.Equal(t, "ociImage", desc.Component.Resources[0].Type)
	assert.Equal(t, descriptor.LocalRelation, desc.Component.Resources[0].Relation)

	// Check sources
	require.Len(t, desc.Component.Sources, 1)
	assert.Equal(t, "example-source", desc.Component.Sources[0].Name)
	assert.Equal(t, "1.0.0", desc.Component.Sources[0].Version)
	assert.Equal(t, "git", desc.Component.Sources[0].Type)

	// Check references
	require.Len(t, desc.Component.References, 1)
	assert.Equal(t, "example-reference", desc.Component.References[0].Name)
	assert.Equal(t, "1.0.0", desc.Component.References[0].Version)
	assert.Equal(t, "other-component", desc.Component.References[0].Component)
}
