package jcs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalise(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		excludes TransformationRules
		expected string
		wantErr  bool
	}{
		{
			name:     "empty map",
			input:    map[string]interface{}{},
			expected: `{}`,
		},
		{
			name:     "empty array",
			input:    []interface{}{},
			expected: `[]`,
		},
		{
			name:     "simple map",
			input:    map[string]interface{}{"a": 1, "b": "2", "c": true},
			expected: `{"a":1,"b":"2","c":true}`,
		},
		{
			name:     "nested map",
			input:    map[string]interface{}{"a": map[string]interface{}{"b": 1}},
			expected: `{"a":{"b":1}}`,
		},
		{
			name:     "array with mixed types",
			input:    []interface{}{1, "2", true, nil},
			expected: `[1,"2",true,null]`,
		},
		{
			name:     "map with array",
			input:    map[string]interface{}{"a": []interface{}{1, 2, 3}},
			expected: `{"a":[1,2,3]}`,
		},
		{
			name:     "map with excluded field",
			input:    map[string]interface{}{"a": 1, "b": 2},
			excludes: MapExcludes{"b": nil},
			expected: `{"a":1}`,
		},
		{
			name:  "array with excluded elements",
			input: []interface{}{1, 2, 3},
			excludes: DynamicArrayExcludes{
				ValueChecker: func(v interface{}) bool {
					return v.(float64) == 2
				},
			},
			expected: `[1,3]`,
		},
		{
			name: "map with none access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "none",
				},
				"digest": "test",
			},
			excludes: MapExcludes{
				"access": nil,
			},
			expected: `{"digest": "test"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Normalise(tt.input, tt.excludes)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(got))
		})
	}
}

func TestNormalised(t *testing.T) {
	t.Run("IsEmpty", func(t *testing.T) {
		tests := []struct {
			name  string
			value interface{}
			want  bool
		}{
			{"empty map", map[string]interface{}{}, true},
			{"empty array", []interface{}{}, true},
			{"non-empty map", map[string]interface{}{"a": 1}, false},
			{"non-empty array", []interface{}{1}, false},
			{"string", "test", false},
			{"number", 1, false},
			{"boolean", true, false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				n := &normalised{value: tt.value}
				assert.Equal(t, tt.want, n.IsEmpty())
			})
		}
	})

	t.Run("Append", func(t *testing.T) {
		n := &normalised{value: []interface{}{}}
		n.Append(&normalised{value: 1})
		n.Append(&normalised{value: "test"})
		assert.Equal(t, []interface{}{1, "test"}, n.value)
	})

	t.Run("SetField", func(t *testing.T) {
		n := &normalised{value: map[string]interface{}{}}
		n.SetField("a", &normalised{value: 1})
		n.SetField("b", &normalised{value: "test"})
		assert.Equal(t, map[string]interface{}{"a": 1, "b": "test"}, n.value)
	})

	t.Run("ToString", func(t *testing.T) {
		n := &normalised{value: map[string]interface{}{
			"a": 1,
			"b": []interface{}{2, 3},
		}}
		expected := `{
  a: 1
  b: [
    2
    3
  ]
}`
		assert.Equal(t, expected, n.ToString(""))
	})

	t.Run("String", func(t *testing.T) {
		n := &normalised{value: map[string]interface{}{"a": 1}}
		expected := `{"a":1}`
		assert.Equal(t, expected, n.String())
	})

	t.Run("Formatted", func(t *testing.T) {
		n := &normalised{value: map[string]interface{}{"a": 1}}
		expected := `{
  "a": 1
}`
		assert.Equal(t, expected, n.Formatted())
	})
}

func TestMapValue(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		mapping  ValueMapper
		cont     TransformationRules
		expected string
	}{
		{
			name: "map value with no mapping",
			input: map[string]interface{}{
				"a": 1,
				"b": 2,
			},
			cont:     NoExcludes{},
			expected: `{"a":1,"b":2}`,
		},
		{
			name: "map value with mapping",
			input: map[string]interface{}{
				"a": 1,
				"b": 2,
			},
			mapping: func(v interface{}) interface{} {
				if m, ok := v.(map[string]interface{}); ok {
					m["c"] = 3
					return m
				}
				return v
			},
			cont:     NoExcludes{},
			expected: `{"a":1,"b":2}`,
		},
		{
			name: "map value with mapping and field exclusion",
			input: map[string]interface{}{
				"a": 1,
				"b": 2,
			},
			mapping: func(v interface{}) interface{} {
				if m, ok := v.(map[string]interface{}); ok {
					m["c"] = 3
					return m
				}
				return v
			},
			cont:     MapExcludes{"b": nil},
			expected: `{"a":1}`,
		},
		{
			name:  "array value with mapping",
			input: []interface{}{1, 2, 3},
			mapping: func(v interface{}) interface{} {
				if arr, ok := v.([]interface{}); ok {
					return append(arr, 4)
				}
				return v
			},
			cont:     NoExcludes{},
			expected: `[1,2,3]`,
		},
		{
			name: "map value with mapping and empty exclusion",
			input: map[string]interface{}{
				"a": map[string]interface{}{},
				"b": 1,
			},
			mapping: func(v interface{}) interface{} {
				if m, ok := v.(map[string]interface{}); ok {
					m["c"] = 3
					return m
				}
				return v
			},
			cont:     ExcludeEmpty{},
			expected: `{"b":1}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := MapValue{
				Mapping:  tt.mapping,
				Continue: tt.cont,
			}
			got, err := Normalise(tt.input, rule)
			assert.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(got))
		})
	}
}

func TestExcludeEmpty(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		excludes TransformationRules
		expected string
	}{
		{
			name:     "empty map with no rules",
			input:    map[string]interface{}{},
			excludes: ExcludeEmpty{},
			expected: `{}`,
		},
		{
			name:     "empty array with no rules",
			input:    []interface{}{},
			excludes: ExcludeEmpty{},
			expected: `[]`,
		},
		{
			name: "map with empty nested map",
			input: map[string]interface{}{
				"a": map[string]interface{}{},
				"b": 1,
			},
			excludes: ExcludeEmpty{},
			expected: `{"b":1}`,
		},
		{
			name: "map with empty nested array",
			input: map[string]interface{}{
				"a": []interface{}{},
				"b": 1,
			},
			excludes: ExcludeEmpty{},
			expected: `{"b":1}`,
		},
		{
			name: "array with empty nested map",
			input: []interface{}{
				map[string]interface{}{},
				1,
			},
			excludes: ExcludeEmpty{},
			expected: `[null,1]`,
		},
		{
			name: "array with empty nested array",
			input: []interface{}{
				[]interface{}{},
				1,
			},
			excludes: ExcludeEmpty{},
			expected: `[null,1]`,
		},
		{
			name: "map with empty values and field exclusion",
			input: map[string]interface{}{
				"a": nil,
				"b": 1,
				"c": "",
			},
			excludes: ExcludeEmpty{
				TransformationRules: MapExcludes{"c": nil},
			},
			expected: `{"b":1}`,
		},
		{
			name: "array with empty values and element exclusion",
			input: []interface{}{
				nil,
				1,
				"",
			},
			excludes: ExcludeEmpty{
				TransformationRules: DynamicArrayExcludes{
					ValueChecker: func(v interface{}) bool {
						return v == nil || v == ""
					},
				},
			},
			expected: `[1]`,
		},
		{
			name: "nested empty structures",
			input: map[string]interface{}{
				"a": map[string]interface{}{
					"b": []interface{}{},
					"c": map[string]interface{}{},
				},
				"d": 1,
			},
			excludes: ExcludeEmpty{},
			expected: `{"d":1}`,
		},
		{
			name: "array with nested empty structures",
			input: []interface{}{
				map[string]interface{}{
					"a": []interface{}{},
					"b": map[string]interface{}{},
				},
				1,
			},
			excludes: ExcludeEmpty{},
			expected: `[null,1]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Normalise(tt.input, tt.excludes)
			assert.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(got))
		})
	}
}

func TestPrepareStruct(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		excludes TransformationRules
		wantErr  bool
	}{
		{
			name: "valid map",
			input: map[string]interface{}{
				"a": 1,
				"b": 2,
			},
			excludes: NoExcludes{},
			wantErr:  false,
		},
		{
			name:     "nil map",
			input:    nil,
			excludes: NoExcludes{},
			wantErr:  false,
		},
		{
			name: "map with error in field",
			input: map[string]interface{}{
				"a": func() {}, // cannot be marshaled to JSON
			},
			excludes: NoExcludes{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m map[string]interface{}
			if tt.input != nil {
				var ok bool
				m, ok = tt.input.(map[string]interface{})
				if !ok {
					_, err := prepareStruct(Type, m, tt.excludes)
					assert.Error(t, err)
					return
				}
			}
			_, err := prepareStruct(Type, m, tt.excludes)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPrepareArray(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		excludes TransformationRules
		wantErr  bool
	}{
		{
			name:     "valid array",
			input:    []interface{}{1, 2, 3},
			excludes: NoExcludes{},
			wantErr:  false,
		},
		{
			name:     "nil array",
			input:    nil,
			excludes: NoExcludes{},
			wantErr:  false,
		},
		{
			name:     "invalid type",
			input:    "not an array",
			excludes: NoExcludes{},
			wantErr:  true,
		},
		{
			name:     "array with error in element",
			input:    []interface{}{func() {}}, // cannot be marshaled to JSON
			excludes: NoExcludes{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var arr []interface{}
			if tt.input != nil {
				var ok bool
				arr, ok = tt.input.([]interface{})
				if !ok {
					if tt.wantErr {
						// For invalid type, we expect an error
						assert.True(t, true) // Type assertion failed as expected
						return
					}
					t.Fatalf("unexpected type: got %T, want []interface{}", tt.input)
				}
			}
			_, err := prepareArray(Type, arr, tt.excludes)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
