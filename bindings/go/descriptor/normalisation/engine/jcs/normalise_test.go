package jcs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalise(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		excludes ExcludeRules
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
		{
			name: "labels with signature",
			input: []interface{}{
				map[string]interface{}{
					"name":    "test",
					"value":   "value",
					"signing": true,
				},
				map[string]interface{}{
					"name":    "test2",
					"value":   "value2",
					"signing": false,
				},
			},
			excludes: LabelExcludes,
			expected: `[{"name":"test","signing":true,"value":"value"}]`,
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

func TestMapResourcesWithNoneAccess(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name: "none access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "none",
				},
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "none",
				},
			},
		},
		{
			name: "legacy none access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "None",
				},
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "None",
				},
			},
		},
		{
			name: "other access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "other",
				},
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "other",
				},
				"digest": "test",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MapResourcesWithNoneAccess(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestIgnoreLabelsWithoutSignature(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected bool
	}{
		{
			name: "with signature true",
			input: map[string]interface{}{
				"signing": true,
			},
			expected: false,
		},
		{
			name: "with signature string true",
			input: map[string]interface{}{
				"signing": "true",
			},
			expected: false,
		},
		{
			name: "with signature false",
			input: map[string]interface{}{
				"signing": false,
			},
			expected: true,
		},
		{
			name: "with signature string false",
			input: map[string]interface{}{
				"signing": "false",
			},
			expected: true,
		},
		{
			name: "without signature",
			input: map[string]interface{}{
				"other": "value",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IgnoreLabelsWithoutSignature(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}
