package parser

import (
	"slices"
	"testing"

	"ocm.software/open-component-model/bindings/go/cel/expression/fieldpath"
	"ocm.software/open-component-model/bindings/go/cel/expression/variable"
)

func areEqualExpressionFields(a, b []variable.FieldDescriptor) bool {
	if len(a) != len(b) {
		return false
	}
	sort := func(x, y variable.FieldDescriptor) int {
		return fieldpath.ComparePaths(x.Path, y.Path)
	}
	slices.SortFunc(a, sort)
	slices.SortFunc(b, sort)

	for i := range a {
		if !equalStrings(a[i].Expressions, b[i].Expressions) || !a[i].Path.Equals(b[i].Path) ||
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
					Path:                 fieldpath.MustParse("field"),
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
					Path:                 fieldpath.MustParse("outer.inner"),
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
					Path:                 fieldpath.MustParse("array[0]"),
					StandaloneExpression: true,
				},
				{
					Expressions:          []string{"array[1]"},
					Path:                 fieldpath.MustParse("array[1]"),
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
					Path:        fieldpath.MustParse("field"),
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
					Path:                 fieldpath.MustParse("string"),
					StandaloneExpression: true,
				},
				{
					Expressions:          []string{"array.value"},
					Path:                 fieldpath.MustParse("nested.array[0]"),
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
					Path:                 fieldpath.MustParse("level1.level2.level3.level4"),
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
					Path:                 fieldpath.MustParse("array[0]"),
					StandaloneExpression: true,
				},
				{
					Expressions:          []string{"expr2"},
					Path:                 fieldpath.MustParse("array[3].nested"),
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
					Path:                 fieldpath.MustParse("empty1"),
					StandaloneExpression: true,
				},
				{
					Expressions:          []string{"    "},
					Path:                 fieldpath.MustParse("empty2"),
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
					Path:                 fieldpath.MustParse("string"),
					StandaloneExpression: true,
				},
				{
					Expressions:          []string{"array.value"},
					Path:                 fieldpath.MustParse("nested.array[0]"),
					StandaloneExpression: true,
				},
				{
					Expressions: []string{"expr1", "expr2"},
					Path:        fieldpath.MustParse("complex.field"),
				},
				{
					Expressions:          []string{"nested.value"},
					Path:                 fieldpath.MustParse("complex.nested.inner"),
					StandaloneExpression: true,
				},
				{
					Expressions:          []string{"expr4"},
					Path:                 fieldpath.MustParse("complex.array[1]"),
					StandaloneExpression: true,
				},
				{
					Expressions:          []string{"expr5"},
					Path:                 fieldpath.MustParse("complex.array[2]"),
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
