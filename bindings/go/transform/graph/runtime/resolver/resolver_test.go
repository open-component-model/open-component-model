package resolver

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
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
			r := NewResolver(tt.resource, nil)
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
			r := NewResolver(tt.resource, nil)
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
			r := NewResolver(tt.resource, tt.data)
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

	r := NewResolver(resource, data)
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
			r := NewResolver(tt.resource, tt.data)
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
