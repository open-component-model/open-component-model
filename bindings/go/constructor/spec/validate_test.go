package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			name: "valid JSON",
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
								"relation": "local"
							}
						]
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
			}
		})
	}
}

func TestJSONSchemaCompilation(t *testing.T) {
	// Test that the JSON schema can be compiled successfully
	schema, err := getJSONSchema()
	require.NoError(t, err)
	require.NotNil(t, schema)

	// Test that the schema is cached
	schema2, err := getJSONSchema()
	require.NoError(t, err)
	require.Equal(t, schema, schema2, "Schema should be cached and return the same instance")
}
