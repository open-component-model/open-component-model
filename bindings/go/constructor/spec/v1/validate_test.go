package v1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		desc    *ComponentConstructor
		wantErr bool
	}{
		{
			name: "valid component constructor",
			desc: &ComponentConstructor{
				Components: []Component{
					{
						ComponentMeta: ComponentMeta{
							ObjectMeta: ObjectMeta{
								Name:    "github.com/acme.org/helloworld",
								Version: "1.0.0",
							},
						},
						Provider: Provider{
							Name: "test-provider",
						},
						Resources: []Resource{
							{
								ElementMeta: ElementMeta{
									ObjectMeta: ObjectMeta{
										Name:    "test-resource",
										Version: "1.0.0",
									},
								},
								Type:     "blob",
								Relation: LocalRelation,
								AccessOrInput: AccessOrInput{Input: &runtime.Raw{
									Type: runtime.Type{
										Version: "v1alpha1",
										Name:    "Typ",
									},
									Data: []byte(`{"type": "Typ/v1alpha1"}`),
								}},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid component constructor - missing required fields",
			desc: &ComponentConstructor{
				Components: []Component{
					{
						ComponentMeta: ComponentMeta{
							ObjectMeta: ObjectMeta{
								Name: "github.com/acme.org/helloworld",
								// Missing version
							},
						},
						// Missing provider
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid component constructor - nil components",
			desc: &ComponentConstructor{
				Components: nil,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.desc)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateRawJSON(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{
			name: "valid JSON (multi)",
			json: `{
				"components": [
					{
						"name": "github.com/acme.org/helloworld",
						"version": "1.0.0",
						"provider": {
							"name": "test-provider"
						},
						"resources": [
							{
								"name": "test-resource",
								"version": "1.0.0",
								"type": "blob",
								"relation": "local",
                                "input": {
									"type": "typ/v1alpha1"
                                }
							},
							{
								"name": "test-resource2",
								"version": "1.0.0",
								"type": "blob",
								"relation": "external",
                                "access": {
									"type": "typ/v1alpha1"
                                }
							}
						]
					}
				]
			}`,
			wantErr: false,
		},
		{
			name: "valid JSON (single)",
			json: `{
						"name": "github.com/acme.org/helloworld",
						"version": "1.0.0",
						"provider": {
							"name": "test-provider"
						},
						"resources": [
							{
								"name": "test-resource",
								"version": "1.0.0",
								"type": "blob",
								"relation": "local",
                                "input": {
									"type": "typ/v1alpha1"
                                }
							},
							{
								"name": "test-resource2",
								"version": "1.0.0",
								"type": "blob",
								"relation": "external",
                                "access": {
									"type": "typ/v1alpha1"
                                }
							}
						]
					}`,
			wantErr: false,
		},
		{
			name: "invalid JSON - missing required fields",
			json: `{
				"components": [
					{
						"name": "github.com/acme.org/helloworld"
						// Missing version and provider
					}
				]
			}`,
			wantErr: true,
		},
		{
			name:    "invalid JSON - malformed",
			json:    `{invalid json}`,
			wantErr: true,
		},
		{
			name: "invalid JSON - missing components",
			json: `{
			}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRawJSON([]byte(tt.json))
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				var desc ComponentConstructor
				assert.NoError(t, json.Unmarshal([]byte(tt.json), &desc))
			}
		})
	}
}

func TestValidateRawYAML(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name: "valid YAML",
			yaml: `
components:
  - name: github.com/acme.org/helloworld
    version: 1.0.0
    provider:
      name: test-provider
    resources:
      - name: test-resource
        version: 1.0.0
        type: blob
        relation: local
        input:
          type: typ/v1alpha1
`,
			wantErr: false,
		},
		{
			name: "valid YAML",
			yaml: `
name: github.com/acme.org/helloworld
version: 1.0.0
provider:
  name: test-provider
resources:
- name: test-resource
  version: 1.0.0
  type: blob
  relation: local
  input:
    type: typ/v1alpha1
`,
			wantErr: false,
		},
		{
			name: "invalid YAML - missing required fields",
			yaml: `
components:
  - name: github.com/acme.org/helloworld
    # Missing version and provider
`,
			wantErr: true,
		},
		{
			name:    "invalid YAML - malformed",
			yaml:    `invalid: yaml: :`,
			wantErr: true,
		},
		{
			name: "invalid YAML - missing components",
			yaml: `
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRawYAML([]byte(tt.yaml))
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				var desc ComponentConstructor
				assert.NoError(t, yaml.Unmarshal([]byte(tt.yaml), &desc))
			}
		})
	}
}

func TestJSONSchemaCompilation(t *testing.T) {
	// Test that the JSON schema can be compiled successfully
	schema, err := GetJSONSchema()
	require.NoError(t, err)
	require.NotNil(t, schema)

	// Test that the schema is cached
	schema2, err := GetJSONSchema()
	require.NoError(t, err)
	require.Equal(t, schema, schema2, "Schema should be cached and return the same instance")
}
