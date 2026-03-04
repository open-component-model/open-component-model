package resolver

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/cel/expression/fieldpath"
	"ocm.software/open-component-model/bindings/go/cel/expression/variable"
)

func TestGetValueFromPath(t *testing.T) {
	tests := []struct {
		name     string
		resource map[string]interface{}
		path     string
		want     interface{}
		wantErr  bool
	}{
		{
			name: "simple field",
			resource: map[string]interface{}{
				"field": "prefix${value1}suffix${value2}",
			},
			path:    "field",
			want:    "prefix${value1}suffix${value2}",
			wantErr: false,
		},
		{
			name: "nested field",
			resource: map[string]interface{}{
				"spec": map[string]interface{}{
					"template": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"image": "${image.name}:${image.tag}",
							},
						},
					},
				},
			},
			path:    `spec["template"]["containers"][0]["image"]`,
			want:    "${image.name}:${image.tag}",
			wantErr: false,
		},
		{
			name: "array access",
			resource: map[string]interface{}{
				"items": []interface{}{
					"${value1}",
					"${value2}",
					"${value3}",
				},
			},
			path:    "items[1]",
			want:    "${value2}",
			wantErr: false,
		},
		{
			name: "mixed quotes and dots",
			resource: map[string]interface{}{
				"spec": map[string]interface{}{
					"my.field.name": "${complex.value}",
				},
			},
			path:    `spec["my.field.name"]`,
			want:    "${complex.value}",
			wantErr: false,
		},
		{
			name: "deep nested arrays",
			resource: map[string]interface{}{
				"metadata": map[string]interface{}{
					"annotations": []interface{}{
						map[string]interface{}{
							"values": []interface{}{
								"${annotation1}",
								"${annotation2}",
							},
						},
					},
				},
			},
			path:    `metadata["annotations"][0]["values"][1]`,
			want:    "${annotation2}",
			wantErr: false,
		},
		{
			name: "nonexistent key",
			resource: map[string]interface{}{
				"field": "${value}",
			},
			path:    "nonexistent",
			want:    nil,
			wantErr: true,
		},
		{
			name: "invalid array index",
			resource: map[string]interface{}{
				"items": []interface{}{"${value}"},
			},
			path:    "items[10]",
			want:    nil,
			wantErr: true,
		},
		{
			name: "invalid type conversion",
			resource: map[string]interface{}{
				"field":        "${value}",
				"field.nested": "invalid",
			},
			path:    "field.nested",
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewResolver(tt.resource, nil, nil)
			got, err := r.getValueFromPath(fieldpath.MustParse(tt.path))

			if (err != nil) != tt.wantErr {
				t.Errorf("getValueFromPath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && got != tt.want {
				t.Errorf("getValueFromPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetValueAtPath(t *testing.T) {
	tests := []struct {
		name     string
		resource map[string]interface{}
		path     string
		value    interface{}
		wantErr  bool
		want     map[string]interface{}
	}{
		{
			name:     "set top level field",
			resource: map[string]interface{}{},
			path:     "name",
			value:    "test-value",
			want: map[string]interface{}{
				"name": "test-value",
			},
		},
		{
			name: "set nested field",
			resource: map[string]interface{}{
				"spec": map[string]interface{}{},
			},
			path:  `spec.replicas`,
			value: 3,
			want: map[string]interface{}{
				"spec": map[string]interface{}{
					"replicas": 3,
				},
			},
		},
		{
			name:     "create intermediate structures",
			resource: map[string]interface{}{},
			path:     `spec.template.metadata.name`,
			value:    "my-pod",
			want: map[string]interface{}{
				"spec": map[string]interface{}{
					"template": map[string]interface{}{
						"metadata": map[string]interface{}{
							"name": "my-pod",
						},
					},
				},
			},
		},
		{
			name:     "create intermediate structures - quoted field names",
			resource: map[string]interface{}{},
			path:     `spec.template.metadata.annotations["custom.annotation.name"]`,
			value:    "my-pod",
			want: map[string]interface{}{
				"spec": map[string]interface{}{
					"template": map[string]interface{}{
						"metadata": map[string]interface{}{
							"annotations": map[string]interface{}{
								"custom.annotation.name": "my-pod",
							},
						},
					},
				},
			},
		},
		{
			name: "set array element",
			resource: map[string]interface{}{
				"containers": []interface{}{
					map[string]interface{}{"name": "container1"},
				},
			},
			path:  "containers[1]",
			value: map[string]interface{}{"name": "container2"},
			want: map[string]interface{}{
				"containers": []interface{}{
					map[string]interface{}{"name": "container1"},
					map[string]interface{}{"name": "container2"},
				},
			},
		},
		{
			name:     "create array and set element",
			resource: map[string]interface{}{},
			path:     `spec.containers[0].ports[0].containerPort`,
			value:    8080,
			want: map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"ports": []interface{}{
								map[string]interface{}{
									"containerPort": 8080,
								},
							},
						},
					},
				},
			},
		},
		{
			name: "extend existing array",
			resource: map[string]interface{}{
				"args": []interface{}{"arg1"},
			},
			path:  "args[2]",
			value: "arg3",
			want: map[string]interface{}{
				"args": []interface{}{
					"arg1",
					nil,
					"arg3",
				},
			},
		},
		{
			name: "overwrite existing value",
			resource: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "old-name",
				},
			},
			path:  `metadata["name"]`,
			value: "new-name",
			want: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "new-name",
				},
			},
		},
		{
			name:     "nested arrays and field at the end",
			resource: map[string]interface{}{},
			path:     `matrix[0][0][0].value`,
			value:    "test",
			want: map[string]interface{}{
				"matrix": []interface{}{
					[]interface{}{
						[]interface{}{
							map[string]interface{}{
								"value": "test",
							},
						},
					},
				},
			},
		},
		{
			name: "nested arraaaaays",
			resource: map[string]interface{}{
				"matrix": []interface{}{
					[]interface{}{},
				},
			},
			value: "catch-me-if-you-can",
			path:  `matrix[0][0][0][0][3]`,
			want: map[string]interface{}{
				"matrix": []interface{}{
					[]interface{}{
						[]interface{}{
							[]interface{}{
								[]interface{}{
									nil,
									nil,
									nil,
									"catch-me-if-you-can",
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewResolver(tt.resource, nil, nil)
			err := r.setValueAtPath(fieldpath.MustParse(tt.path), tt.value)

			if (err != nil) != tt.wantErr {
				t.Errorf("setValueAtPath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && !reflect.DeepEqual(tt.resource, tt.want) {
				t.Errorf("setValueAtPath() got = %v, want %v", tt.resource, tt.want)
			}
		})
	}
}

func TestResolveField(t *testing.T) {
	tests := []struct {
		name     string
		resource map[string]interface{}
		data     map[string]interface{}
		field    variable.FieldDescriptor
		want     ResolutionResult
	}{
		{
			name: "non data provided",
			resource: map[string]interface{}{
				"spec": map[string]interface{}{
					"field": "${notProvided}",
				},
			},
			field: variable.FieldDescriptor{
				Path: fieldpath.MustParse("spec.field"),
				Expressions: []variable.Expression{
					{Value: "notProvided"},
				},
				StandaloneExpression: true,
			},
			want: ResolutionResult{
				Path:     fieldpath.MustParse("spec.field"),
				Original: "[notProvided]",
				Resolved: false,
				Error:    fmt.Errorf("no data provided for expression: notProvided"),
			},
		},
		{
			name: "standalone expression simple path",
			resource: map[string]interface{}{
				"spec": map[string]interface{}{
					"field": "${value}",
				},
			},
			data: map[string]interface{}{
				"value": []float64{1, 2, 3.5},
			},
			field: variable.FieldDescriptor{
				Path: fieldpath.MustParse("spec.field"),
				Expressions: []variable.Expression{
					{Value: "value"},
				},
				StandaloneExpression: true,
			},
			want: ResolutionResult{
				Path:     fieldpath.MustParse("spec.field"),
				Original: "[value]",
				Resolved: true,
				Replaced: []float64{1, 2, 3.5},
			},
		},
		{
			name: "multiple expressions in string",
			resource: map[string]interface{}{
				"spec": map[string]interface{}{
					"field": "prefix-${value1}-${value2}-suffix",
				},
			},
			data: map[string]interface{}{
				"value1": "one",
				"value2": "two",
			},
			field: variable.FieldDescriptor{
				Path: fieldpath.MustParse("spec.field"),
				Expressions: []variable.Expression{
					{Value: "value1"},
					{Value: "value2"},
				},
			},
			want: ResolutionResult{
				Path:     fieldpath.MustParse("spec.field"),
				Original: "[value1 value2]",
				Resolved: true,
				Replaced: "prefix-one-two-suffix",
			},
		},
		{
			name: "array path with standalone expression",
			resource: map[string]interface{}{
				"spec": map[string]interface{}{
					"array": []interface{}{
						"${value}",
					},
				},
			},
			data: map[string]interface{}{
				"value": "resolved",
			},
			field: variable.FieldDescriptor{
				Path: fieldpath.MustParse("spec.array[0]"),
				Expressions: []variable.Expression{
					{Value: "value"},
				},
				StandaloneExpression: true,
			},
			want: ResolutionResult{
				Path:     fieldpath.MustParse("spec.array[0]"),
				Original: "[value]",
				Resolved: true,
				Replaced: "resolved",
			},
		},
		{
			name: "error - missing data for expression",
			resource: map[string]interface{}{
				"spec": map[string]interface{}{
					"field": "${missing}",
				},
			},
			data: map[string]interface{}{},
			field: variable.FieldDescriptor{
				Path: fieldpath.MustParse("spec.field"),
				Expressions: []variable.Expression{
					{Value: "missing"},
				},
				StandaloneExpression: true,
			},
			want: ResolutionResult{
				Path:     fieldpath.MustParse("spec.field"),
				Original: "[missing]",
				Error:    fmt.Errorf("no data provided for expression: missing"),
			},
		},
		{
			name: "error - invalid path",
			resource: map[string]interface{}{
				"spec": map[string]interface{}{},
			},
			data: map[string]interface{}{
				"value": "resolved",
			},
			field: variable.FieldDescriptor{
				Path: fieldpath.MustParse("spec.nonexistent.field"),
				Expressions: []variable.Expression{
					{Value: "value"},
				},
				StandaloneExpression: true,
			},
			want: ResolutionResult{
				Path:     fieldpath.MustParse("spec.nonexistent.field"),
				Original: "[value]",
				Error:    fmt.Errorf("error getting value: key not found: nonexistent"),
			},
		},
		{
			name: "error - non-string value for template",
			resource: map[string]interface{}{
				"spec": map[string]interface{}{
					"field": 123,
				},
			},
			data: map[string]interface{}{
				"value": "resolved",
			},
			field: variable.FieldDescriptor{
				Path: fieldpath.MustParse("spec.field"),
				Expressions: []variable.Expression{
					{Value: "value"},
				},
			},
			want: ResolutionResult{
				Path:     fieldpath.MustParse("spec.field"),
				Original: "[value]",
				Error:    fmt.Errorf("expected string value for path spec.field"),
			},
		},
		{
			name: "deeply nested array path",
			resource: map[string]interface{}{
				"spec": map[string]interface{}{
					"nested": map[string]interface{}{
						"array": []interface{}{
							map[string]interface{}{
								"field": "${value}",
							},
						},
					},
				},
			},
			data: map[string]interface{}{
				"value": "papa-ou-t-es",
			},
			field: variable.FieldDescriptor{
				Path: fieldpath.MustParse("spec.nested.array[0].field"),
				Expressions: []variable.Expression{
					{Value: "value"},
				},
				StandaloneExpression: true,
			},
			want: ResolutionResult{
				Path:     fieldpath.MustParse("spec.nested.array[0].field"),
				Original: "[value]",
				Resolved: true,
				Replaced: "papa-ou-t-es",
			},
		},
		{
			name: "multiple expressions with different types",
			resource: map[string]interface{}{
				"spec": map[string]interface{}{
					"field": "Count: ${count}, Name: ${name}, Active: ${active}",
				},
			},
			data: map[string]interface{}{
				"count":  42,
				"name":   "test",
				"active": true,
			},
			field: variable.FieldDescriptor{
				Path: fieldpath.MustParse("spec.field"),
				Expressions: []variable.Expression{
					{Value: "count"},
					{Value: "name"},
					{Value: "active"},
				},
			},
			want: ResolutionResult{
				Path:     fieldpath.MustParse("spec.field"),
				Original: "[count name active]",
				Resolved: true,
				Replaced: "Count: 42, Name: test, Active: true",
			},
		},
		{
			name: "nested array with multiple expressions",
			resource: map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{},
						nil,
						map[string]interface{}{
							"image": "${image.name}:${image.tag}",
						},
					},
				},
			},
			data: map[string]interface{}{
				"image.name": "nginx",
				"image.tag":  "latest",
			},
			field: variable.FieldDescriptor{
				Path: fieldpath.MustParse(`spec.containers[2].image`),
				Expressions: []variable.Expression{
					{Value: "image.name"},
					{Value: "image.tag"},
				},
			},
			want: ResolutionResult{
				Path:     fieldpath.MustParse("spec.containers[2].image"),
				Original: "[image.name image.tag]",
				Resolved: true,
				Replaced: "nginx:latest",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewResolver(tt.resource, tt.data, nil)
			got := r.resolveField(tt.field)

			assert.Equal(t, tt.want.Path, got.Path)
			assert.Equal(t, tt.want.Original, got.Original)
			assert.Equal(t, tt.want.Resolved, got.Resolved)
			assert.Equal(t, tt.want.Replaced, got.Replaced)

			if tt.want.Error != nil {
				assert.EqualError(t, got.Error, tt.want.Error.Error())
			} else {
				assert.NoError(t, got.Error)
			}

			if tt.want.Resolved {
				value, err := r.getValueFromPath(tt.field.Path)
				assert.NoError(t, err)
				assert.Equal(t, tt.want.Replaced, value)
			}
		})
	}
}

func TestResolveField_NilOptionalPointer_NilSchema(t *testing.T) {
	resource := map[string]interface{}{
		"field": "${expr}",
	}
	data := map[string]interface{}{
		"expr": nil,
	}

	r := NewResolver(resource, data, nil)
	result := r.resolveField(variable.FieldDescriptor{
		Path:                 fieldpath.MustParse("field"),
		Expressions:          []variable.Expression{{Value: "expr"}},
		StandaloneExpression: true,
	})

	require.NoError(t, result.Error)
	require.True(t, result.Resolved)
	require.False(t, result.Deleted, "Deleted should be false when no schema is present")
	require.Contains(t, resource, "field", "nil value without schema should be kept")
	require.Nil(t, resource["field"])
}

func TestResolveField_NilOptionalPointer_SpecSchema(t *testing.T) {
	// The resolver now receives the spec sub-schema directly (not the full
	// transformation schema). Paths in FieldDescriptors are relative to spec,
	// matching the schema properties exactly.
	refTarget := &jsonschema.Schema{}
	refSchema := &jsonschema.Schema{Ref: refTarget}
	stringSchema := &jsonschema.Schema{}
	numberSchema := &jsonschema.Schema{}

	// Deep nested schema
	tagsTarget := &jsonschema.Schema{}
	tagsSchema := &jsonschema.Schema{Ref: tagsTarget}

	inputSchema := &jsonschema.Schema{
		Properties: map[string]*jsonschema.Schema{
			"optional":    refSchema,
			"endpoint":    stringSchema,
			"credentials": refSchema,
		},
		Required: []string{"endpoint"},
	}
	targetObjTarget := &jsonschema.Schema{}
	targetObjSchema := &jsonschema.Schema{Ref: targetObjTarget}

	annotationsSchema := &jsonschema.Schema{
		Properties: map[string]*jsonschema.Schema{
			"tags": tagsSchema,
			"name": stringSchema,
		},
		Required: []string{"name"},
	}

	// The spec sub-schema (what the resolver's resource maps to directly).
	specSchema := &jsonschema.Schema{
		Properties: map[string]*jsonschema.Schema{
			"input":       {Ref: inputSchema},
			"target":      targetObjSchema,
			"annotations": {Ref: annotationsSchema},
			"replicas":    numberSchema,
		},
		Required: []string{"input", "target"},
	}

	t.Run("strips nil optional ref at depth 1", func(t *testing.T) {
		resource := map[string]interface{}{
			"input": map[string]interface{}{
				"optional": "${optExpr}",
				"endpoint": "https://api.example.com",
			},
			"target":   map[string]interface{}{"name": "my-target"},
			"replicas": 3,
		}
		data := map[string]interface{}{"optExpr": nil}

		r := NewResolver(resource, data, specSchema)
		result := r.resolveField(variable.FieldDescriptor{
			Path:                 fieldpath.MustParse("input.optional"),
			Expressions:          []variable.Expression{{Value: "optExpr"}},
			StandaloneExpression: true,
		})

		require.NoError(t, result.Error)
		require.True(t, result.Resolved)
		require.True(t, result.Deleted, "Deleted should be true when nil optional field is removed")
		input := resource["input"].(map[string]interface{})
		require.NotContains(t, input, "optional", "nil optional $ref should be deleted")
		require.Equal(t, "https://api.example.com", input["endpoint"])
	})

	t.Run("keeps nil for required ref at depth 1", func(t *testing.T) {
		resource := map[string]interface{}{
			"input":  map[string]interface{}{"endpoint": "https://api.example.com"},
			"target": "${targetExpr}",
		}
		data := map[string]interface{}{"targetExpr": nil}

		r := NewResolver(resource, data, specSchema)
		result := r.resolveField(variable.FieldDescriptor{
			Path:                 fieldpath.MustParse("target"),
			Expressions:          []variable.Expression{{Value: "targetExpr"}},
			StandaloneExpression: true,
		})

		require.NoError(t, result.Error)
		require.True(t, result.Resolved)
		require.False(t, result.Deleted, "Deleted should be false when nil required field is kept")
		require.Contains(t, resource, "target", "nil required $ref should be kept")
		require.Nil(t, resource["target"])
	})

	t.Run("strips nil optional ref at depth 2", func(t *testing.T) {
		resource := map[string]interface{}{
			"input":  map[string]interface{}{"endpoint": "https://api.example.com"},
			"target": map[string]interface{}{"name": "my-target"},
			"annotations": map[string]interface{}{
				"tags": "${tagsExpr}",
				"name": "my-annotation",
			},
		}
		data := map[string]interface{}{"tagsExpr": nil}

		r := NewResolver(resource, data, specSchema)
		result := r.resolveField(variable.FieldDescriptor{
			Path:                 fieldpath.MustParse("annotations.tags"),
			Expressions:          []variable.Expression{{Value: "tagsExpr"}},
			StandaloneExpression: true,
		})

		require.NoError(t, result.Error)
		require.True(t, result.Resolved)
		require.True(t, result.Deleted)
		ann := resource["annotations"].(map[string]interface{})
		require.NotContains(t, ann, "tags", "nil optional $ref at depth 2 should be deleted")
		require.Equal(t, "my-annotation", ann["name"])
	})

	t.Run("keeps nil for required string and strips optional ref at depth 2", func(t *testing.T) {
		resource := map[string]interface{}{
			"input": map[string]interface{}{
				"endpoint":    "${endpointExpr}",
				"credentials": "${credExpr}",
			},
			"target": map[string]interface{}{"name": "my-target"},
		}
		data := map[string]interface{}{"endpointExpr": nil, "credExpr": nil}

		r := NewResolver(resource, data, specSchema)

		endpointResult := r.resolveField(variable.FieldDescriptor{
			Path:                 fieldpath.MustParse("input.endpoint"),
			Expressions:          []variable.Expression{{Value: "endpointExpr"}},
			StandaloneExpression: true,
		})
		require.NoError(t, endpointResult.Error)
		require.True(t, endpointResult.Resolved)
		require.False(t, endpointResult.Deleted)

		credResult := r.resolveField(variable.FieldDescriptor{
			Path:                 fieldpath.MustParse("input.credentials"),
			Expressions:          []variable.Expression{{Value: "credExpr"}},
			StandaloneExpression: true,
		})
		require.NoError(t, credResult.Error)
		require.True(t, credResult.Resolved)
		require.True(t, credResult.Deleted)

		input := resource["input"].(map[string]interface{})
		require.Contains(t, input, "endpoint", "nil required string should be kept")
		require.Nil(t, input["endpoint"])
		require.NotContains(t, input, "credentials", "nil optional $ref should be deleted")
	})

	t.Run("strips nil for optional non-ref field", func(t *testing.T) {
		resource := map[string]interface{}{
			"input":    map[string]interface{}{"endpoint": "https://api.example.com"},
			"target":   map[string]interface{}{"name": "my-target"},
			"replicas": "${replicasExpr}",
		}
		data := map[string]interface{}{"replicasExpr": nil}

		r := NewResolver(resource, data, specSchema)
		result := r.resolveField(variable.FieldDescriptor{
			Path:                 fieldpath.MustParse("replicas"),
			Expressions:          []variable.Expression{{Value: "replicasExpr"}},
			StandaloneExpression: true,
		})

		require.NoError(t, result.Error)
		require.True(t, result.Resolved)
		require.True(t, result.Deleted)
		require.NotContains(t, resource, "replicas", "nil optional non-ref field should be deleted")
	})

	t.Run("preserves non-nil values for optional ref fields", func(t *testing.T) {
		optionalData := map[string]interface{}{"key": "val", "count": 42}
		credData := map[string]interface{}{"user": "admin", "pass": "secret"}
		resource := map[string]interface{}{
			"input": map[string]interface{}{
				"optional":    "${optExpr}",
				"endpoint":    "https://api.example.com",
				"credentials": "${credExpr}",
			},
			"target": map[string]interface{}{"name": "my-target"},
		}
		data := map[string]interface{}{"optExpr": optionalData, "credExpr": credData}

		r := NewResolver(resource, data, specSchema)

		optResult := r.resolveField(variable.FieldDescriptor{
			Path:                 fieldpath.MustParse("input.optional"),
			Expressions:          []variable.Expression{{Value: "optExpr"}},
			StandaloneExpression: true,
		})
		require.NoError(t, optResult.Error)
		require.True(t, optResult.Resolved)
		require.False(t, optResult.Deleted)

		credResult := r.resolveField(variable.FieldDescriptor{
			Path:                 fieldpath.MustParse("input.credentials"),
			Expressions:          []variable.Expression{{Value: "credExpr"}},
			StandaloneExpression: true,
		})
		require.NoError(t, credResult.Error)
		require.True(t, credResult.Resolved)
		require.False(t, credResult.Deleted)

		input := resource["input"].(map[string]interface{})
		require.Equal(t, optionalData, input["optional"])
		require.Equal(t, credData, input["credentials"])
	})
}

func TestIsOptionalField_NestedRefs(t *testing.T) {
	stringSchema := &jsonschema.Schema{}

	// innermost: has "host" (required) and "timeout" (optional)
	innerSchema := &jsonschema.Schema{
		Properties: map[string]*jsonschema.Schema{
			"host":    stringSchema,
			"timeout": stringSchema,
		},
		Required: []string{"host"},
	}

	// mid-level ref target: has "connection" (ref to inner, required) and "label" (optional)
	midSchema := &jsonschema.Schema{
		Properties: map[string]*jsonschema.Schema{
			"connection": {Ref: innerSchema},
			"label":      stringSchema,
		},
		Required: []string{"connection"},
	}

	// top-level: "backend" is a ref to mid (required), "description" is optional
	topSchema := &jsonschema.Schema{
		Properties: map[string]*jsonschema.Schema{
			"backend":     {Ref: midSchema},
			"description": stringSchema,
		},
		Required: []string{"backend"},
	}

	r := NewResolver(nil, nil, topSchema)

	assert.False(t, r.isOptionalField(fieldpath.MustParse("backend.connection.host")),
		"host is required inside nested ref chain")
	assert.True(t, r.isOptionalField(fieldpath.MustParse("backend.connection.timeout")),
		"timeout is optional inside nested ref chain")
	assert.True(t, r.isOptionalField(fieldpath.MustParse("backend.label")),
		"label is optional in mid-level ref")
	assert.False(t, r.isOptionalField(fieldpath.MustParse("backend.connection")),
		"connection is required in mid-level ref")
	assert.False(t, r.isOptionalField(fieldpath.MustParse("backend")),
		"backend is required at top level")
	assert.True(t, r.isOptionalField(fieldpath.MustParse("description")),
		"description is optional at top level")

	// --- array paths using Items2020 ---
	itemSchema := &jsonschema.Schema{
		Properties: map[string]*jsonschema.Schema{
			"host":    stringSchema,
			"timeout": stringSchema,
		},
		Required: []string{"host"},
	}

	arrayTopSchema := &jsonschema.Schema{
		Properties: map[string]*jsonschema.Schema{
			"servers": {Items2020: itemSchema},
		},
		Required: []string{"servers"},
	}

	rArr := NewResolver(nil, nil, arrayTopSchema)

	assert.False(t, rArr.isOptionalField(fieldpath.MustParse("servers[0].host")),
		"host is required inside array item (Items2020)")
	assert.True(t, rArr.isOptionalField(fieldpath.MustParse("servers[0].timeout")),
		"timeout is optional inside array item (Items2020)")
	assert.True(t, rArr.isOptionalField(fieldpath.MustParse("servers[2].timeout")),
		"timeout is optional regardless of array index")

	// --- array items with Ref ---
	refTarget := &jsonschema.Schema{
		Properties: map[string]*jsonschema.Schema{
			"port":    stringSchema,
			"comment": stringSchema,
		},
		Required: []string{"port"},
	}
	arrayRefSchema := &jsonschema.Schema{
		Properties: map[string]*jsonschema.Schema{
			"endpoints": {Items2020: &jsonschema.Schema{Ref: refTarget}},
		},
		Required: []string{"endpoints"},
	}

	rRef := NewResolver(nil, nil, arrayRefSchema)

	assert.False(t, rRef.isOptionalField(fieldpath.MustParse("endpoints[0].port")),
		"port is required inside array item with Ref")
	assert.True(t, rRef.isOptionalField(fieldpath.MustParse("endpoints[0].comment")),
		"comment is optional inside array item with Ref")
}

func TestResolveDynamicArrayIndexes(t *testing.T) {
	resource := map[string]interface{}{
		"spec": map[string]interface{}{
			"array": []interface{}{
				"value1",
				"${value}",
				"value3",
			},
		},
	}

	data := map[string]interface{}{
		"value": "replaced",
	}

	field := variable.FieldDescriptor{
		Path: fieldpath.MustParse("spec.array[1]"),
		Expressions: []variable.Expression{
			{Value: "value"},
		},
		StandaloneExpression: true,
	}

	r := NewResolver(resource, data, nil)
	got := r.resolveField(field)

	assert.True(t, got.Resolved)
	assert.Equal(t, "replaced", got.Replaced)

	array, ok := r.resource["spec"].(map[string]interface{})["array"].([]interface{})
	assert.True(t, ok)

	assert.Equal(t, "value1", array[0])
	assert.Equal(t, "replaced", array[1])
	assert.Equal(t, "value3", array[2])
}

func TestResolver(t *testing.T) {
	r := NewResolver(
		map[string]interface{}{
			"spec": map[string]interface{}{
				"field": "${value}-${suffix}",
			},
		},
		map[string]interface{}{
			"value":  "resolved",
			"suffix": "done",
		},
		nil,
	)

	summary := r.Resolve([]variable.FieldDescriptor{
		{
			Path: fieldpath.MustParse("spec.field"),
			Expressions: []variable.Expression{
				{Value: "value"},
				{Value: "suffix"},
			},
		},
	})

	assert.Equal(t, summary.TotalExpressions, 1)
	assert.Equal(t, summary.ResolvedExpressions, 1)
	assert.Equal(t, "resolved-done", summary.Results[0].Replaced)
}

func TestResolveFieldWithEmptyBraces(t *testing.T) {
	tests := []struct {
		name     string
		resource map[string]interface{}
		data     map[string]interface{}
		field    variable.FieldDescriptor
		want     ResolutionResult
	}{
		{
			name: "standalone expression ending with empty braces",
			resource: map[string]interface{}{
				"metadata": map[string]interface{}{
					"annotations": "${includeAnnotations ? annotations : {}}",
				},
			},
			data: map[string]interface{}{
				"includeAnnotations ? annotations : {}": map[string]interface{}{},
			},
			field: variable.FieldDescriptor{
				Path: fieldpath.MustParse("metadata.annotations"),
				Expressions: []variable.Expression{
					{Value: "includeAnnotations ? annotations : {}"},
				},
				StandaloneExpression: true,
			},
			want: ResolutionResult{
				Path:     fieldpath.MustParse("metadata.annotations"),
				Original: "[includeAnnotations ? annotations : {}]",
				Resolved: true,
				Replaced: map[string]interface{}{},
			},
		},
		{
			name: "complex expression with has() and empty braces",
			resource: map[string]interface{}{
				"spec": map[string]interface{}{
					"config": "${has(schema.config) && includeConfig ? schema.config : {}}",
				},
			},
			data: map[string]interface{}{
				"has(schema.config) && includeConfig ? schema.config : {}": map[string]interface{}{
					"key": "value",
				},
			},
			field: variable.FieldDescriptor{
				Path: fieldpath.MustParse("spec.config"),
				Expressions: []variable.Expression{
					{Value: "has(schema.config) && includeConfig ? schema.config : {}"},
				},
				StandaloneExpression: true,
			},
			want: ResolutionResult{
				Path:     fieldpath.MustParse("spec.config"),
				Original: "[has(schema.config) && includeConfig ? schema.config : {}]",
				Resolved: true,
				Replaced: map[string]interface{}{
					"key": "value",
				},
			},
		},
		{
			name: "expression with empty braces on both sides",
			resource: map[string]interface{}{
				"data": map[string]interface{}{
					"field": "${condition ? {} : {}}",
				},
			},
			data: map[string]interface{}{
				"condition ? {} : {}": map[string]interface{}{},
			},
			field: variable.FieldDescriptor{
				Path: fieldpath.MustParse("data.field"),
				Expressions: []variable.Expression{
					{Value: "condition ? {} : {}"},
				},
				StandaloneExpression: true,
			},
			want: ResolutionResult{
				Path:     fieldpath.MustParse("data.field"),
				Original: "[condition ? {} : {}]",
				Resolved: true,
				Replaced: map[string]interface{}{},
			},
		},
		{
			name: "string template with expression ending in braces",
			resource: map[string]interface{}{
				"spec": map[string]interface{}{
					"value": "prefix-${expr ? value : {}}-suffix",
				},
			},
			data: map[string]interface{}{
				"expr ? value : {}": "resolved",
			},
			field: variable.FieldDescriptor{
				Path: fieldpath.MustParse("spec.value"),
				Expressions: []variable.Expression{
					{Value: "expr ? value : {}"},
				},
			},
			want: ResolutionResult{
				Path:     fieldpath.MustParse("spec.value"),
				Original: "[expr ? value : {}]",
				Resolved: true,
				Replaced: "prefix-resolved-suffix",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewResolver(tt.resource, tt.data, nil)
			got := r.resolveField(tt.field)

			assert.Equal(t, tt.want.Path, got.Path)
			assert.Equal(t, tt.want.Original, got.Original)
			assert.Equal(t, tt.want.Resolved, got.Resolved)
			assert.Equal(t, tt.want.Replaced, got.Replaced)

			if tt.want.Error != nil {
				assert.EqualError(t, got.Error, tt.want.Error.Error())
			} else {
				assert.NoError(t, got.Error)
			}

			if tt.want.Resolved {
				value, err := r.getValueFromPath(tt.field.Path)
				assert.NoError(t, err)
				assert.Equal(t, tt.want.Replaced, value)
			}
		})
	}
}

func TestDeleteValueAtPath(t *testing.T) {
	tests := []struct {
		name     string
		resource map[string]interface{}
		path     string
		wantErr  bool
		want     map[string]interface{}
	}{
		{
			name: "delete nested field",
			resource: map[string]interface{}{
				"spec": map[string]interface{}{
					"replicas": 3,
					"selector": "app=test",
				},
			},
			path: `spec.replicas`,
			want: map[string]interface{}{
				"spec": map[string]interface{}{
					"selector": "app=test",
				},
			},
		},
		{
			name:     "empty path is no-op",
			resource: map[string]interface{}{"key": "value"},
			path:     "",
			want:     map[string]interface{}{"key": "value"},
		},
		{
			name: "error when intermediate path is not a map",
			resource: map[string]interface{}{
				"spec": "not-a-map",
			},
			path:    `spec.replicas`,
			wantErr: true,
		},
		{
			name: "delete field inside array element",
			resource: map[string]interface{}{
				"items": []interface{}{
					map[string]interface{}{
						"key":   "keep",
						"extra": "remove",
					},
				},
			},
			path: `items[0].extra`,
			want: map[string]interface{}{
				"items": []interface{}{
					map[string]interface{}{
						"key": "keep",
					},
				},
			},
		},
		{
			name: "delete field inside nested array element",
			resource: map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{"name": "c0"},
						map[string]interface{}{
							"name":     "c1",
							"optional": "gone",
						},
					},
				},
			},
			path: `spec.containers[1].optional`,
			want: map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{"name": "c0"},
						map[string]interface{}{"name": "c1"},
					},
				},
			},
		},
		{
			name: "delete field inside array element nested in array",
			resource: map[string]interface{}{
				"matrix": []interface{}{
					[]interface{}{
						map[string]interface{}{
							"required": "stay",
							"optional": "remove",
						},
					},
				},
			},
			path: `matrix[0][0].optional`,
			want: map[string]interface{}{
				"matrix": []interface{}{
					[]interface{}{
						map[string]interface{}{
							"required": "stay",
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewResolver(tt.resource, nil, nil)

			var path fieldpath.Path
			if tt.path != "" {
				path = fieldpath.MustParse(tt.path)
			}

			err := r.deleteValueAtPath(path)

			if (err != nil) != tt.wantErr {
				t.Errorf("deleteValueAtPath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && !reflect.DeepEqual(tt.resource, tt.want) {
				t.Errorf("deleteValueAtPath() got = %v, want %v", tt.resource, tt.want)
			}
		})
	}
}
