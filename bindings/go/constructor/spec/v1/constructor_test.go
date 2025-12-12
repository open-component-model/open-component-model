package v1_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const jsonArrayData = `{
  "components": [
    {
      "name": "github.com/example/component",
      "version": "v1.0.0",
      "provider": {
        "name": "example-provider"
      },
      "resources": [
        {
          "name": "example-resource",
          "version": "v1.0.0",
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
          "version": "v1.0.0",
          "type": "git",
          "access": {
            "type": "gitHub",
            "repoUrl": "https://github.com/example/repo"
          }
        }
      ],
      "componentReferences": [
        {
          "name": "example-reference",
          "version": "v1.0.0",
          "componentName": "github.com/example/other-component"
        }
      ]
    }
  ]
}`

const jsonSingleData = `{
  "name": "github.com/example/component",
  "version": "v1.0.0",
  "provider": {
    "name": "example-provider"
  },
  "resources": [
    {
      "name": "example-resource",
      "version": "v1.0.0",
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
      "version": "v1.0.0",
      "type": "git",
      "access": {
        "type": "gitHub",
        "repoUrl": "https://github.com/example/repo"
      }
    }
  ],
  "componentReferences": [
    {
      "name": "example-reference",
      "version": "v1.0.0",
      "componentName": "github.com/example/other-component"
    }
  ]
}`

func TestComponentConstructor_UnmarshalJSON(t *testing.T) {
	t.Run("ArrayForm", func(t *testing.T) {
		var constructor v1.ComponentConstructor
		err := json.Unmarshal([]byte(jsonArrayData), &constructor)
		require.NoError(t, err)
		require.Len(t, constructor.Components, 1)
		assert.Equal(t, "github.com/example/component", constructor.Components[0].Name)
		assert.Equal(t, "v1.0.0", constructor.Components[0].Version)
	})

	t.Run("SingleComponentForm", func(t *testing.T) {
		var constructor v1.ComponentConstructor
		err := json.Unmarshal([]byte(jsonSingleData), &constructor)
		require.NoError(t, err)
		require.Len(t, constructor.Components, 1)
		assert.Equal(t, "github.com/example/component", constructor.Components[0].Name)
		assert.Equal(t, "v1.0.0", constructor.Components[0].Version)
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		var constructor v1.ComponentConstructor
		err := json.Unmarshal([]byte(`invalid json`), &constructor)
		assert.Error(t, err)
	})
}

func TestAccessOrInput_Validate(t *testing.T) {
	tests := []struct {
		name          string
		accessOrInput v1.AccessOrInput
		expectError   bool
	}{
		{
			name: "ValidAccess",
			accessOrInput: v1.AccessOrInput{
				Access: &runtime.Raw{
					Type: runtime.Type{Name: "ociArtifact"},
					Data: []byte(`{"type":"ociArtifact","imageReference":"test/image:1.0"}`),
				},
			},
			expectError: false,
		},
		{
			name: "ValidInput",
			accessOrInput: v1.AccessOrInput{
				Input: &runtime.Raw{
					Type: runtime.Type{Name: "ociArtifact"},
					Data: []byte(`{"type":"ociArtifact","imageReference":"test/image:1.0"}`),
				},
			},
			expectError: false,
		},
		{
			name:          "NeitherAccessNorInput",
			accessOrInput: v1.AccessOrInput{},
			expectError:   true,
		},
		{
			name: "BothAccessAndInput",
			accessOrInput: v1.AccessOrInput{
				Access: &runtime.Raw{
					Type: runtime.Type{Name: "ociArtifact"},
					Data: []byte(`{"type":"ociArtifact","imageReference":"test/image:1.0"}`),
				},
				Input: &runtime.Raw{
					Type: runtime.Type{Name: "ociArtifact"},
					Data: []byte(`{"type":"ociArtifact","imageReference":"test/image:1.0"}`),
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.accessOrInput.Validate()
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestObjectMeta_String(t *testing.T) {
	tests := []struct {
		name     string
		objMeta  v1.ObjectMeta
		expected string
	}{
		{
			name: "WithNameOnly",
			objMeta: v1.ObjectMeta{
				Name: "test-object",
			},
			expected: "test-object",
		},
		{
			name: "WithNameAndVersion",
			objMeta: v1.ObjectMeta{
				Name:    "test-object",
				Version: "1.0.0",
			},
			expected: "test-object:1.0.0",
		},
		{
			name: "WithNameVersionAndLabels",
			objMeta: v1.ObjectMeta{
				Name:    "test-object",
				Version: "1.0.0",
				Labels: []v1.Label{
					{Name: "type", Value: []byte("library")},
					{Name: "priority", Value: []byte("high")},
				},
			},
			expected: "test-object:1.0.0+labels([label{type=library} label{priority=high}])",
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
	elemMeta := v1.ElementMeta{
		ObjectMeta: v1.ObjectMeta{
			Name:    "test-element",
			Version: "2.0.0",
			Labels: []v1.Label{
				{Name: "type", Value: []byte("backend")},
			},
		},
		ExtraIdentity: runtime.Identity{
			"namespace": "system",
			"platform":  "linux",
		},
	}

	result := elemMeta.String()
	assert.Contains(t, result, "test-element:2.0.0")
	assert.Contains(t, result, "+labels([label{type=backend}])")
	assert.Contains(t, result, "+extraIdentity(")
	assert.Contains(t, result, "namespace=system")
	assert.Contains(t, result, "platform=linux")
}

func TestElementMeta_ToIdentity(t *testing.T) {
	tests := []struct {
		name     string
		elemMeta *v1.ElementMeta
		expected runtime.Identity
	}{
		{
			name: "WithExtraIdentity",
			elemMeta: &v1.ElementMeta{
				ObjectMeta: v1.ObjectMeta{
					Name:    "test-element",
					Version: "2.0.0",
				},
				ExtraIdentity: runtime.Identity{
					"namespace": "system",
				},
			},
			expected: runtime.Identity{
				"name":      "test-element",
				"version":   "2.0.0",
				"namespace": "system",
			},
		},
		{
			name: "WithoutExtraIdentity",
			elemMeta: &v1.ElementMeta{
				ObjectMeta: v1.ObjectMeta{
					Name:    "test-element",
					Version: "2.0.0",
				},
			},
			expected: runtime.Identity{
				"name":    "test-element",
				"version": "2.0.0",
			},
		},
		{
			name:     "NilElementMeta",
			elemMeta: nil,
			expected: nil,
		},
		{
			name: "WithoutVersion",
			elemMeta: &v1.ElementMeta{
				ObjectMeta: v1.ObjectMeta{
					Name: "test",
				},
			},
			expected: runtime.Identity{
				v1.IdentityAttributeName: "test",
			},
		},
		{
			name: "WithoutName",
			elemMeta: &v1.ElementMeta{
				ObjectMeta: v1.ObjectMeta{
					Version: "test",
				},
			},
			expected: runtime.Identity{
				v1.IdentityAttributeVersion: "test",
			},
		},
		{
			name: "WithoutAnything",
			elemMeta: &v1.ElementMeta{
				ObjectMeta: v1.ObjectMeta{},
			},
			expected: runtime.Identity{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity := tt.elemMeta.ToIdentity()
			assert.Equal(t, tt.expected, identity)
		})
	}
}

func TestComponentMeta_ToIdentity(t *testing.T) {
	tests := []struct {
		name     string
		compMeta *v1.ComponentMeta
		expected runtime.Identity
	}{
		{
			name: "WithNameAndVersion",
			compMeta: &v1.ComponentMeta{
				ObjectMeta: v1.ObjectMeta{
					Name:    "test-component",
					Version: "3.0.0",
				},
			},
			expected: runtime.Identity{
				"name":    "test-component",
				"version": "3.0.0",
			},
		},
		{
			name:     "NilComponentMeta",
			compMeta: nil,
			expected: nil,
		},
		{
			name: "NameWithoutVersion",
			compMeta: &v1.ComponentMeta{
				ObjectMeta: v1.ObjectMeta{
					Name: "test-component",
				},
			},
			expected: runtime.Identity{
				v1.IdentityAttributeName: "test-component",
			},
		},
		{
			name: "VersionWithoutName",
			compMeta: &v1.ComponentMeta{
				ObjectMeta: v1.ObjectMeta{
					Version: "1.0.0",
				},
			},
			expected: runtime.Identity{
				v1.IdentityAttributeVersion: "1.0.0",
			},
		},
		{
			name: "EmptyComponentMeta",
			compMeta: &v1.ComponentMeta{
				ObjectMeta: v1.ObjectMeta{},
			},
			expected: runtime.Identity{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity := tt.compMeta.ToIdentity()
			assert.Equal(t, tt.expected, identity)
		})
	}
}

func TestResource_Struct(t *testing.T) {
	resource := v1.Resource{
		ElementMeta: v1.ElementMeta{
			ObjectMeta: v1.ObjectMeta{
				Name:    "test-resource",
				Version: "1.0.0",
			},
		},
		Type:     "ociImage",
		Relation: v1.LocalRelation,
		AccessOrInput: v1.AccessOrInput{
			Access: &runtime.Raw{
				Type: runtime.Type{Name: "ociArtifact"},
				Data: []byte(`{"type":"ociArtifact","imageReference":"test/image:1.0"}`),
			},
		},
	}

	jsonData, err := json.Marshal(resource)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"name":"test-resource"`)
	assert.Contains(t, string(jsonData), `"version":"1.0.0"`)
	assert.Contains(t, string(jsonData), `"type":"ociImage"`)
	assert.Contains(t, string(jsonData), `"relation":"local"`)
	assert.Contains(t, string(jsonData), `"access":{"type":"ociArtifact","imageReference":"test/image:1.0"}`)
}

func TestSource_Struct(t *testing.T) {
	source := v1.Source{
		ElementMeta: v1.ElementMeta{
			ObjectMeta: v1.ObjectMeta{
				Name:    "test-source",
				Version: "1.0.0",
			},
		},
		Type: "git",
		AccessOrInput: v1.AccessOrInput{
			Access: &runtime.Raw{
				Type: runtime.Type{Name: "gitHub"},
				Data: []byte(`{"type":"gitHub","repoUrl":"https://github.com/test/repo"}`),
			},
		},
	}

	jsonData, err := json.Marshal(source)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"name":"test-source"`)
	assert.Contains(t, string(jsonData), `"version":"1.0.0"`)
	assert.Contains(t, string(jsonData), `"type":"git"`)
	assert.Contains(t, string(jsonData), `"access":{"type":"gitHub","repoUrl":"https://github.com/test/repo"}`)
}

func TestReference_Struct(t *testing.T) {
	reference := v1.Reference{
		ElementMeta: v1.ElementMeta{
			ObjectMeta: v1.ObjectMeta{
				Name:    "test-reference",
				Version: "1.0.0",
			},
		},
		Component: "referenced-component",
	}

	jsonData, err := json.Marshal(reference)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"name":"test-reference"`)
	assert.Contains(t, string(jsonData), `"version":"1.0.0"`)
	assert.Contains(t, string(jsonData), `"componentName":"referenced-component"`)
}

func TestProvider_Struct(t *testing.T) {
	provider := v1.Provider{
		Name: "test-provider",
		Labels: []v1.Label{
			{Name: "type", Value: []byte(`"infrastructure"`)},
		},
	}

	jsonData, err := json.Marshal(provider)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"name":"test-provider"`)
	assert.Contains(t, string(jsonData), `"name":"type","value":"infrastructure"`)
}

func TestLabel_Struct(t *testing.T) {
	label := v1.Label{
		Name:    "environment",
		Value:   []byte(`"production"`),
		Signing: true,
	}

	jsonData, err := json.Marshal(label)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"name":"environment"`)
	assert.Contains(t, string(jsonData), `"value":"production"`)
	assert.Contains(t, string(jsonData), `"signing":true`)
}
