package parser

import (
	"sort"
	"testing"

	"ocm.software/open-component-model/bindings/go/cel/expression/variable"
)

func areEqualExpressionFields(a, b []variable.FieldDescriptor) bool {
	if len(a) != len(b) {
		return false
	}

	sort.Slice(a, func(i, j int) bool { return a[i].Path < a[j].Path })
	sort.Slice(b, func(i, j int) bool { return b[i].Path < b[j].Path })

	for i := range a {
		if !equalStrings(a[i].Expressions, b[i].Expressions) ||
			a[i].Path != b[i].Path ||
			a[i].StandaloneExpression != b[i].StandaloneExpression {
			return false
		}
	}
	return true
}

func TestParseSchemalessResource(t *testing.T) {
	tests := []struct {
		name     string
		resource map[string]interface{}
		want     []variable.FieldDescriptor
		wantErr  bool
	}{
		{
			name: "Simple string field",
			resource: map[string]interface{}{
				"field": "${resource.value}",
			},
			want: []variable.FieldDescriptor{
				{
					Expressions:          []string{"resource.value"},
					Path:                 "field",
					StandaloneExpression: true,
				},
			},
			wantErr: false,
		},
		{
			name: "Nested map",
			resource: map[string]interface{}{
				"outer": map[string]interface{}{
					"inner": "${nested.value}",
				},
			},
			want: []variable.FieldDescriptor{
				{
					Expressions:          []string{"nested.value"},
					Path:                 "outer.inner",
					StandaloneExpression: true,
				},
			},
			wantErr: false,
		},
		{
			name: "array field",
			resource: map[string]interface{}{
				"array": []interface{}{
					"${array[0]}",
					"${array[1]}",
				},
			},
			want: []variable.FieldDescriptor{
				{
					Expressions:          []string{"array[0]"},
					Path:                 "array[0]",
					StandaloneExpression: true,
				},
				{
					Expressions:          []string{"array[1]"},
					Path:                 "array[1]",
					StandaloneExpression: true,
				},
			},
			wantErr: false,
		},
		{
			name: "Multiple expressions in string",
			resource: map[string]interface{}{
				"field": "Start ${expr1} middle ${expr2} end",
			},
			want: []variable.FieldDescriptor{
				{
					Expressions: []string{"expr1", "expr2"},
					Path:        "field",
				},
			},
			wantErr: false,
		},
		{
			name: "Mixed types",
			resource: map[string]interface{}{
				"string": "${string.value}",
				"number": 42,
				"bool":   true,
				"nested": map[string]interface{}{
					"array": []interface{}{
						"${array.value}",
						123,
					},
				},
			},
			want: []variable.FieldDescriptor{
				{
					Expressions:          []string{"string.value"},
					Path:                 "string",
					StandaloneExpression: true,
				},
				{
					Expressions:          []string{"array.value"},
					Path:                 "nested.array[0]",
					StandaloneExpression: true,
				},
			},
			wantErr: false,
		},
		{
			name:     "Empty resource",
			resource: map[string]interface{}{},
			want:     []variable.FieldDescriptor{},
			wantErr:  false,
		},
		{
			name: "Nested expression (should error)",
			resource: map[string]interface{}{
				"field": "${outer(${inner})}",
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSchemaless(tt.resource)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSchemaless() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !areEqualExpressionFields(got, tt.want) {
				t.Errorf("ParseSchemaless() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseSchemalessResourceEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		resource map[string]interface{}
		want     []variable.FieldDescriptor
		wantErr  bool
	}{
		{
			name: "Deeply nested structure",
			resource: map[string]interface{}{
				"level1": map[string]interface{}{
					"level2": map[string]interface{}{
						"level3": map[string]interface{}{
							"level4": "${deeply.nested.value}",
						},
					},
				},
			},
			want: []variable.FieldDescriptor{
				{
					Expressions:          []string{"deeply.nested.value"},
					Path:                 "level1.level2.level3.level4",
					StandaloneExpression: true,
				},
			},
			wantErr: false,
		},
		{
			name: "Array with mixed types",
			resource: map[string]interface{}{
				"array": []interface{}{
					"${expr1}",
					42,
					true,
					map[string]interface{}{
						"nested": "${expr2}",
					},
				},
			},
			want: []variable.FieldDescriptor{
				{
					Expressions:          []string{"expr1"},
					Path:                 "array[0]",
					StandaloneExpression: true,
				},
				{
					Expressions:          []string{"expr2"},
					Path:                 "array[3].nested",
					StandaloneExpression: true,
				},
			},
			wantErr: false,
		},
		{
			name: "Empty string expressions",
			resource: map[string]interface{}{
				"empty1": "${}",
				"empty2": "${    }",
			},
			want: []variable.FieldDescriptor{
				{
					Expressions:          []string{""},
					Path:                 "empty1",
					StandaloneExpression: true,
				},
				{
					Expressions:          []string{"    "},
					Path:                 "empty2",
					StandaloneExpression: true,
				},
			},
			wantErr: false,
		},
		{
			name: "Incomplete expressions",
			resource: map[string]interface{}{
				"incomplete1": "${incomplete",
				"incomplete2": "incomplete}",
				"incomplete3": "$not_an_expression",
			},
			want:    []variable.FieldDescriptor{},
			wantErr: false,
		},
		{
			name: "Complex strcture with various expressions combinations",
			resource: map[string]interface{}{
				"string": "${string.value}",
				"number": 42,
				"bool":   true,
				"nested": map[string]interface{}{
					"array": []interface{}{
						"${array.value}",
						123,
					},
				},
				"complex": map[string]interface{}{
					"field": "Start ${expr1} middle ${expr2} end",
					"nested": map[string]interface{}{
						"inner": "${nested.value}",
					},
					"array": []interface{}{
						"${expr3-incmplete",
						"${expr4}",
						"${expr5}",
					},
				},
			},
			want: []variable.FieldDescriptor{
				{
					Expressions:          []string{"string.value"},
					Path:                 "string",
					StandaloneExpression: true,
				},
				{
					Expressions:          []string{"array.value"},
					Path:                 "nested.array[0]",
					StandaloneExpression: true,
				},
				{
					Expressions: []string{"expr1", "expr2"},
					Path:        "complex.field",
				},
				{
					Expressions:          []string{"nested.value"},
					Path:                 "complex.nested.inner",
					StandaloneExpression: true,
				},
				{
					Expressions:          []string{"expr4"},
					Path:                 "complex.array[1]",
					StandaloneExpression: true,
				},
				{
					Expressions:          []string{"expr5"},
					Path:                 "complex.array[2]",
					StandaloneExpression: true,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSchemaless(tt.resource)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSchemaless() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !areEqualExpressionFields(got, tt.want) {
				t.Errorf("ParseSchemaless() = %v, want %v", got, tt.want)
			}
		})
	}
}
