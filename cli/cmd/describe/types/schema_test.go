package types

import (
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/cel/expression/fieldpath"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// compileTestSchema compiles a JSON schema string for testing.
func compileTestSchema(t *testing.T, schemaJSON string) *jsonschema.Schema {
	t.Helper()
	unmarshaled, err := jsonschema.UnmarshalJSON(strings.NewReader(schemaJSON))
	require.NoError(t, err, "failed to unmarshal schema JSON")

	compiler := jsonschema.NewCompiler()
	require.NoError(t, compiler.AddResource("test", unmarshaled), "failed to add resource")

	compiled, err := compiler.Compile("test")
	require.NoError(t, err, "failed to compile schema")
	return compiled
}

func TestBuildSchemaInfo(t *testing.T) {
	tests := []struct {
		name       string
		schemaJSON string
		typ        runtime.Type
		path       fieldpath.Path
		expected   schemaInfo
	}{
		{
			name: "schema with title and description",
			schemaJSON: `{
				"type": "object",
				"title": "File Input",
				"description": "Reads content from a file",
				"properties": {
					"path": {"type": "string"},
					"mediaType": {"type": "string"}
				},
				"required": ["path"]
			}`,
			typ:  runtime.Type{Name: "file", Version: "v1"},
			path: nil,
			expected: schemaInfo{
				Type:        "file/v1",
				Title:       "File Input",
				Description: "Reads content from a file",
				Deprecated:  false,
				FieldCount:  2,
				Required:    1,
				Optional:    1,
				Breadcrumb:  "",
			},
		},
		{
			name: "schema without title falls back to type",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"url": {"type": "string"}
				}
			}`,
			typ:  runtime.Type{Name: "http", Version: "v1"},
			path: nil,
			expected: schemaInfo{
				Type:        "http/v1",
				Title:       "http/v1",
				Description: "",
				Deprecated:  false,
				FieldCount:  1,
				Required:    0,
				Optional:    1,
				Breadcrumb:  "",
			},
		},
		{
			name: "schema with path generates breadcrumb",
			schemaJSON: `{
				"type": "object",
				"title": "Nested Config",
				"properties": {
					"value": {"type": "string"}
				}
			}`,
			typ: runtime.Type{Name: "config", Version: "v1"},
			path: fieldpath.Path{
				{Name: "spec"},
				{Name: "nested"},
			},
			expected: schemaInfo{
				Type:        "config/v1",
				Title:       "Nested Config",
				Description: "",
				Deprecated:  false,
				FieldCount:  1,
				Required:    0,
				Optional:    1,
				Breadcrumb:  "config/v1 > spec > nested",
			},
		},
		{
			name: "deprecated schema",
			schemaJSON: `{
				"type": "object",
				"deprecated": true,
				"properties": {}
			}`,
			typ:  runtime.Type{Name: "old", Version: "v1"},
			path: nil,
			expected: schemaInfo{
				Type:        "old/v1",
				Title:       "old/v1",
				Description: "",
				Deprecated:  true,
				FieldCount:  0,
				Required:    0,
				Optional:    0,
				Breadcrumb:  "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := compileTestSchema(t, tt.schemaJSON)
			info := buildSchemaInfo(tt.typ, schema, tt.path)

			assert.Equal(t, tt.expected.Type, info.Type)
			assert.Equal(t, tt.expected.Title, info.Title)
			assert.Equal(t, tt.expected.Description, info.Description)
			assert.Equal(t, tt.expected.Deprecated, info.Deprecated)
			assert.Equal(t, tt.expected.FieldCount, info.FieldCount)
			assert.Equal(t, tt.expected.Required, info.Required)
			assert.Equal(t, tt.expected.Optional, info.Optional)
			assert.Equal(t, tt.expected.Breadcrumb, info.Breadcrumb)
		})
	}
}

func TestSchemaInfoFormatTableTitle(t *testing.T) {
	tests := []struct {
		name     string
		info     schemaInfo
		contains []string
	}{
		{
			name: "root level with matching title and type",
			info: schemaInfo{
				Type:       "file/v1",
				Title:      "file/v1",
				FieldCount: 2,
				Required:   1,
				Optional:   1,
			},
			contains: []string{"file/v1", "2 fields", "1 required", "1 optional"},
		},
		{
			name: "root level with different title",
			info: schemaInfo{
				Type:        "file/v1",
				Title:       "File Input",
				Description: "Input from file",
				FieldCount:  3,
				Required:    2,
				Optional:    1,
			},
			contains: []string{"File Input", "(file/v1)", "Input from file", "3 fields"},
		},
		{
			name: "with breadcrumb",
			info: schemaInfo{
				Type:       "file/v1",
				Title:      "Spec",
				Breadcrumb: "file/v1 > spec",
				FieldCount: 1,
				Required:   0,
				Optional:   1,
			},
			contains: []string{"file/v1 > spec", "1 fields"},
		},
		{
			name: "deprecated type",
			info: schemaInfo{
				Type:       "old/v1",
				Title:      "old/v1",
				Deprecated: true,
				FieldCount: 0,
			},
			contains: []string{"old/v1", "WARNING", "deprecated"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title := tt.info.formatTableTitle()
			for _, expected := range tt.contains {
				assert.Contains(t, title, expected)
			}
		})
	}
}

func TestNavigateFieldPath(t *testing.T) {
	tests := []struct {
		name        string
		schemaJSON  string
		path        string
		expectError bool
		errorMsg    string
	}{
		{
			name: "empty path returns root",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"name": {"type": "string"}
				}
			}`,
			path:        "",
			expectError: false,
		},
		{
			name: "single segment navigation",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"spec": {
						"type": "object",
						"properties": {
							"value": {"type": "string"}
						}
					}
				}
			}`,
			path:        "spec",
			expectError: false,
		},
		{
			name: "multi-segment navigation",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"spec": {
						"type": "object",
						"properties": {
							"nested": {
								"type": "object",
								"properties": {
									"deep": {"type": "string"}
								}
							}
						}
					}
				}
			}`,
			path:        "spec.nested",
			expectError: false,
		},
		{
			name: "non-existent field",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"name": {"type": "string"},
					"value": {"type": "number"}
				}
			}`,
			path:        "nonexistent",
			expectError: true,
			errorMsg:    "not found",
		},
		{
			name: "error message lists available fields",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"alpha": {"type": "string"},
					"beta": {"type": "string"},
					"gamma": {"type": "string"}
				}
			}`,
			path:        "delta",
			expectError: true,
			errorMsg:    "alpha",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := compileTestSchema(t, tt.schemaJSON)

			var path fieldpath.Path
			if tt.path != "" {
				var err error
				path, err = fieldpath.Parse(tt.path)
				require.NoError(t, err)
			}

			result, err := navigateFieldPath(schema, path)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestGetPropertyTypeString(t *testing.T) {
	tests := []struct {
		name       string
		schemaJSON string
		propName   string
		expected   string
	}{
		{
			name: "simple string type",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"name": {"type": "string"}
				}
			}`,
			propName: "name",
			expected: "[string]",
		},
		{
			name: "number type",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"count": {"type": "number"}
				}
			}`,
			propName: "count",
			expected: "[number]",
		},
		{
			name: "boolean type",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"enabled": {"type": "boolean"}
				}
			}`,
			propName: "enabled",
			expected: "[boolean]",
		},
		{
			name: "object with properties shows arrow",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"config": {
						"type": "object",
						"properties": {
							"key": {"type": "string"}
						}
					}
				}
			}`,
			propName: "config",
			expected: "[object] →",
		},
		{
			name: "object without properties no arrow",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"data": {"type": "object"}
				}
			}`,
			propName: "data",
			expected: "[object]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := compileTestSchema(t, tt.schemaJSON)
			prop := schema.Properties[tt.propName]
			require.NotNil(t, prop, "property %s not found", tt.propName)

			result := getPropertyTypeString(prop)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetPropertyDescription(t *testing.T) {
	tests := []struct {
		name       string
		schemaJSON string
		propName   string
		contains   []string
	}{
		{
			name: "direct description",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"path": {
						"type": "string",
						"description": "The file path to read"
					}
				}
			}`,
			propName: "path",
			contains: []string{"file path to read"},
		},
		{
			name: "enum values appended",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"format": {
						"type": "string",
						"description": "Output format",
						"enum": ["json", "yaml", "text"]
					}
				}
			}`,
			propName: "format",
			contains: []string{"Output format", "Possible values"},
		},
		{
			name: "oneOf const values",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"mode": {
						"type": "string",
						"description": "Operation mode",
						"oneOf": [
							{"const": "read"},
							{"const": "write"},
							{"const": "append"}
						]
					}
				}
			}`,
			propName: "mode",
			contains: []string{"Operation mode", "Possible values", "read", "write", "append"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := compileTestSchema(t, tt.schemaJSON)
			prop := schema.Properties[tt.propName]
			require.NotNil(t, prop)

			result := getPropertyDescription(prop)
			for _, expected := range tt.contains {
				assert.Contains(t, result, expected)
			}
		})
	}
}

func TestCollectFieldPaths(t *testing.T) {
	tests := []struct {
		name       string
		schemaJSON string
		maxDepth   int
		expected   []string
	}{
		{
			name: "flat schema",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"alpha": {"type": "string"},
					"beta": {"type": "string"},
					"gamma": {"type": "string"}
				}
			}`,
			maxDepth: 5,
			expected: []string{"alpha", "beta", "gamma"},
		},
		{
			name: "nested schema",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"spec": {
						"type": "object",
						"properties": {
							"config": {
								"type": "object",
								"properties": {
									"value": {"type": "string"}
								}
							}
						}
					}
				}
			}`,
			maxDepth: 5,
			expected: []string{"spec", "spec.config", "spec.config.value"},
		},
		{
			name: "depth limit respected",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"level1": {
						"type": "object",
						"properties": {
							"level2": {
								"type": "object",
								"properties": {
									"level3": {"type": "string"}
								}
							}
						}
					}
				}
			}`,
			maxDepth: 2,
			expected: []string{"level1", "level1.level2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := compileTestSchema(t, tt.schemaJSON)
			result := collectFieldPaths(schema, "", tt.maxDepth)

			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSchemaHasProperties(t *testing.T) {
	tests := []struct {
		name       string
		schemaJSON string
		expected   bool
	}{
		{
			name: "schema with properties",
			schemaJSON: `{
				"type": "object",
				"properties": {
					"name": {"type": "string"}
				}
			}`,
			expected: true,
		},
		{
			name: "schema without properties",
			schemaJSON: `{
				"type": "string"
			}`,
			expected: false,
		},
		{
			name: "empty properties object",
			schemaJSON: `{
				"type": "object",
				"properties": {}
			}`,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := compileTestSchema(t, tt.schemaJSON)
			result := schemaHasProperties(schema)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetFieldConstraints(t *testing.T) {
	tests := []struct {
		name       string
		schemaJSON string
		contains   []string
		empty      bool
	}{
		{
			name: "schema with enum",
			schemaJSON: `{
				"type": "string",
				"enum": ["a", "b", "c"]
			}`,
			contains: []string{"Possible values"},
		},
		{
			name: "schema with oneOf const",
			schemaJSON: `{
				"type": "string",
				"oneOf": [
					{"const": "option1"},
					{"const": "option2"}
				]
			}`,
			contains: []string{"Possible values", "option1", "option2"},
		},
		{
			name: "schema without constraints",
			schemaJSON: `{
				"type": "string"
			}`,
			empty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := compileTestSchema(t, tt.schemaJSON)
			result := getFieldConstraints(schema)

			if tt.empty {
				assert.Empty(t, result)
			} else {
				for _, expected := range tt.contains {
					assert.Contains(t, result, expected)
				}
			}
		})
	}
}
