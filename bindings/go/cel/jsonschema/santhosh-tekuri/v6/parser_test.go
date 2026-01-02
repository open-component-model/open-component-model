package jsonschema_test

import (
	"slices"
	"testing"

	"github.com/google/cel-go/common/types"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/cel/expression/fieldpath"
	"ocm.software/open-component-model/bindings/go/cel/expression/variable"
	stv6jsonschema "ocm.software/open-component-model/bindings/go/cel/jsonschema/santhosh-tekuri/v6"
)

func TestParseResource(t *testing.T) {
	t.Run("Simple resource with various types", func(t *testing.T) {
		resource := map[string]interface{}{
			"stringField": "${string.value}",
			"intField":    "${int.value}",
			"boolField":   "${bool.value}",
			"nestedObject": map[string]interface{}{
				"nestedString":         "${nested.string}",
				"nestedStringMultiple": "${nested.string1}-${nested.string2}",
			},
			"simpleArray": []interface{}{
				"${array[0]}",
				"${array[1]}",
			},
			"mapField": map[string]interface{}{
				"key1": "${map.key1}",
				"key2": "${map.key2}",
			},
			"specialCharacters": map[string]interface{}{
				"simpleAnnotation":     "${simpleannotation}",
				"doted.annotation.key": "${dotedannotationvalue}",
				"array.name.with.dots": []interface{}{
					"${value}",
				},
			},
			"schemalessField": map[string]interface{}{
				"key":       "value",
				"something": "${schemaless.value}",
				"nestedSomething": map[string]interface{}{
					"key":    "value",
					"nested": "${schemaless.nested.value}",
				},
			},
		}

		schema := &jsonschema.Schema{
			Types: stv6jsonschema.TypeForSchema("object"),
			Properties: map[string]*jsonschema.Schema{
				"stringField": {Types: stv6jsonschema.TypeForSchema("string")},
				"intField":    {Types: stv6jsonschema.TypeForSchema("integer")},
				"boolField":   {Types: stv6jsonschema.TypeForSchema("boolean")},
				"nestedObject": {
					Types: stv6jsonschema.TypeForSchema("object"),
					Properties: map[string]*jsonschema.Schema{
						"nestedString":         {Types: stv6jsonschema.TypeForSchema("string")},
						"nestedStringMultiple": {Types: stv6jsonschema.TypeForSchema("string")},
					},
				},
				"simpleArray": {
					Types: stv6jsonschema.TypeForSchema("array"),
					Items2020: &jsonschema.Schema{
						Types: stv6jsonschema.TypeForSchema("string"),
					},
				},
				"mapField": {
					Types: stv6jsonschema.TypeForSchema("object"),
					AdditionalProperties: &jsonschema.Schema{
						Types: stv6jsonschema.TypeForSchema("string"),
					},
				},
				"specialCharacters": {
					Types: stv6jsonschema.TypeForSchema("object"),
					Properties: map[string]*jsonschema.Schema{
						"simpleAnnotation":     {Types: stv6jsonschema.TypeForSchema("string")},
						"doted.annotation.key": {Types: stv6jsonschema.TypeForSchema("string")},
						"array.name.with.dots": {
							Types: stv6jsonschema.TypeForSchema("array"),
							Items2020: &jsonschema.Schema{
								Types: stv6jsonschema.TypeForSchema("string"),
							},
						},
					},
				},
				"schemalessField": {
					Types: stv6jsonschema.TypeForSchema("object"),
				},
			},
		}

		expected := []variable.FieldDescriptor{
			{Path: fieldpath.MustParse("stringField"), Expressions: []variable.Expression{{Value: "string.value"}}, StandaloneExpression: true},
			{Path: fieldpath.MustParse("intField"), Expressions: []variable.Expression{{Value: "int.value"}}, StandaloneExpression: true},
			{Path: fieldpath.MustParse("boolField"), Expressions: []variable.Expression{{Value: "bool.value"}}, StandaloneExpression: true},
			{Path: fieldpath.MustParse("nestedObject.nestedString"), Expressions: []variable.Expression{{Value: "nested.string"}}, StandaloneExpression: true},
			{Path: fieldpath.MustParse("nestedObject.nestedStringMultiple"),
				Expressions: []variable.Expression{
					{Value: "nested.string1"},
					{Value: "nested.string2"},
				},
				StandaloneExpression: false,
			},
			{Path: fieldpath.MustParse("simpleArray[0]"), Expressions: []variable.Expression{{Value: "array[0]"}}, StandaloneExpression: true},
			{Path: fieldpath.MustParse("simpleArray[1]"), Expressions: []variable.Expression{{Value: "array[1]"}}, StandaloneExpression: true},
			{Path: fieldpath.MustParse("mapField.key1"), Expressions: []variable.Expression{{Value: "map.key1"}}, StandaloneExpression: true},
			{Path: fieldpath.MustParse("mapField.key2"), Expressions: []variable.Expression{{Value: "map.key2"}}, StandaloneExpression: true},
			{Path: fieldpath.MustParse("specialCharacters.simpleAnnotation"), Expressions: []variable.Expression{{Value: "simpleannotation"}}, StandaloneExpression: true},
			{Path: fieldpath.MustParse(`specialCharacters["doted.annotation.key"]`), Expressions: []variable.Expression{{Value: "dotedannotationvalue"}}, StandaloneExpression: true},
			{Path: fieldpath.MustParse(`specialCharacters["array.name.with.dots"][0]`), Expressions: []variable.Expression{{Value: "value"}}, StandaloneExpression: true},
			{Path: fieldpath.MustParse("schemalessField.something"), Expressions: []variable.Expression{{Value: "schemaless.value"}}, StandaloneExpression: true},
			{Path: fieldpath.MustParse("schemalessField.nestedSomething.nested"), Expressions: []variable.Expression{{Value: "schemaless.nested.value"}}, StandaloneExpression: true},
		}

		got, err := stv6jsonschema.ParseResource(resource, schema)
		require.NoError(t, err, "ParseResource should not error")

		sortFn := func(a, b variable.FieldDescriptor) int {
			return fieldpath.Compare(a.Path, b.Path)
		}
		slices.SortFunc(got, sortFn)
		slices.SortFunc(expected, sortFn)

		require.Equal(t, len(expected), len(got), "number of expressions mismatch")

		for i := range expected {
			exp := expected[i]
			act := got[i]

			assert.True(t, act.Path.Equals(exp.Path),
				"path mismatch:\n  got: %s\n want: %s", act.Path, exp.Path)

			assert.Equal(t, len(exp.Expressions), len(act.Expressions),
				"expression count mismatch at path %s", exp.Path)

			for j := range exp.Expressions {
				assert.Equal(t, exp.Expressions[j].Value, act.Expressions[j].Value,
					"expression[%d] mismatch at path %s", j, exp.Path)
			}

			assert.Equal(t, exp.StandaloneExpression, act.StandaloneExpression,
				"StandaloneExpression mismatch at %s", exp.Path)
		}
	})
}

func TestTypeMismatches(t *testing.T) {
	testCases := []struct {
		name          string
		resource      map[string]interface{}
		schema        *jsonschema.Schema
		wantErr       bool
		expectedError string
	}{
		{
			name: "String instead of integer",
			resource: map[string]interface{}{
				"intField": "not an int",
			},
			schema: &jsonschema.Schema{
				Types: stv6jsonschema.TypeForSchema("object"),
				Properties: map[string]*jsonschema.Schema{
					"intField": {Types: stv6jsonschema.TypeForSchema("integer")},
				},
			},
			wantErr:       true,
			expectedError: "jsonschema validation failed with ''\n- at '/intField': got string, want integer",
		},
		{
			name: "Integer instead of string",
			resource: map[string]interface{}{
				"stringField": 123,
			},
			schema: &jsonschema.Schema{
				Types: stv6jsonschema.TypeForSchema("object"),
				Properties: map[string]*jsonschema.Schema{
					"stringField": {Types: stv6jsonschema.TypeForSchema("string")},
				},
			},
			wantErr:       true,
			expectedError: "jsonschema validation failed with ''\n- at '/stringField': got number, want string",
		},
		{
			name: "Boolean instead of number",
			resource: map[string]interface{}{
				"numberField": true,
			},
			schema: &jsonschema.Schema{
				Types: stv6jsonschema.TypeForSchema("object"),
				Properties: map[string]*jsonschema.Schema{
					"numberField": {Types: stv6jsonschema.TypeForSchema("number")},
				},
			},
			wantErr:       true,
			expectedError: "jsonschema validation failed with ''\n- at '/numberField': got boolean, want number",
		},
		{
			name: "Array instead of object",
			resource: map[string]interface{}{
				"objectField": []interface{}{"not", "an", "object"},
			},
			schema: &jsonschema.Schema{
				Types: stv6jsonschema.TypeForSchema("object"),
				Properties: map[string]*jsonschema.Schema{
					"objectField": {Types: stv6jsonschema.TypeForSchema("object")},
				},
			},
			wantErr:       true,
			expectedError: "jsonschema validation failed with ''\n- at '/objectField': got array, want object",
		},
		{
			name: "Object instead of array",
			resource: map[string]interface{}{
				"arrayField": map[string]interface{}{"key": "value"},
			},
			schema: &jsonschema.Schema{
				Types: stv6jsonschema.TypeForSchema("object"),
				Properties: map[string]*jsonschema.Schema{
					"arrayField": {Types: stv6jsonschema.TypeForSchema("array")},
				},
			},
			wantErr:       true,
			expectedError: "jsonschema validation failed with ''\n- at '/arrayField': got object, want array",
		},
		{
			name: "Nested field type mismatch",
			resource: map[string]interface{}{
				"level1": map[string]interface{}{
					"level2": map[string]interface{}{
						"numberField": "not-a-number",
					},
				},
			},
			schema: &jsonschema.Schema{
				Types: stv6jsonschema.TypeForSchema("object"),
				Properties: map[string]*jsonschema.Schema{
					"level1": {
						Types: stv6jsonschema.TypeForSchema("object"),
						Properties: map[string]*jsonschema.Schema{
							"level2": {
								Types: stv6jsonschema.TypeForSchema("object"),
								Properties: map[string]*jsonschema.Schema{
									"numberField": {
										Types: stv6jsonschema.TypeForSchema("number"),
									},
								},
							},
						},
					},
				},
			},
			wantErr:       true,
			expectedError: "jsonschema validation failed with ''\n- at '/level1/level2/numberField': got string, want number",
		},
		{
			name: "Nil schema",
			resource: map[string]interface{}{
				"field": "value",
			},
			schema:  nil,
			wantErr: true,
		},
		{
			name: "Schema with OneOf, valid",
			resource: map[string]interface{}{
				"field": "value",
			},
			schema: &jsonschema.Schema{
				Types: stv6jsonschema.TypeForSchema("object"),
				Properties: map[string]*jsonschema.Schema{
					"field": {
						OneOf: []*jsonschema.Schema{
							{Types: stv6jsonschema.TypeForSchema("string")},
							{Types: stv6jsonschema.TypeForSchema("integer")},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Schema with empty type list",
			resource: map[string]interface{}{
				"field": "value",
			},
			schema:        &jsonschema.Schema{},
			wantErr:       true,
			expectedError: "cannot create type information from schema, unsupported schema structure",
		},
		{
			name: "Valid types (no mismatch)",
			resource: map[string]interface{}{
				"stringField": "valid string",
				"intField":    42,
				"boolField":   true,
				"numberField": 3.14,
				"objectField": map[string]interface{}{"key": "value"},
				"arrayField":  []interface{}{1, 2, 3},
			},
			schema: &jsonschema.Schema{
				Types: stv6jsonschema.TypeForSchema("object"),
				Properties: map[string]*jsonschema.Schema{
					"stringField": {Types: stv6jsonschema.TypeForSchema("string")},
					"intField":    {Types: stv6jsonschema.TypeForSchema("integer")},
					"boolField":   {Types: stv6jsonschema.TypeForSchema("boolean")},
					"numberField": {Types: stv6jsonschema.TypeForSchema("number")},
					"objectField": {
						Types: stv6jsonschema.TypeForSchema("object"),
						Properties: map[string]*jsonschema.Schema{
							"key": {Types: stv6jsonschema.TypeForSchema("string")},
						},
					},
					"arrayField": {
						Types: stv6jsonschema.TypeForSchema("array"),
						Items2020: &jsonschema.Schema{
							Types: stv6jsonschema.TypeForSchema("integer"),
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := stv6jsonschema.ParseResource(tc.resource, tc.schema)

			if tc.wantErr {
				require.Error(t, err, "expected error but got none")
				if tc.expectedError != "" {
					assert.ErrorContains(t, err, tc.expectedError)
				}
			} else {
				require.NoError(t, err, "did not expect an error")
			}
		})
	}
}

func TestParseWithExpectedSchema(t *testing.T) {
	resource := map[string]interface{}{
		"stringField": "${string.value}",
		"objectField": "${object.value}",
		"nestedObjectField": map[string]interface{}{
			"nestedString": "${nested.string}",
			"nestedObject": map[string]interface{}{
				"deepNested": "${deep.nested}",
			},
		},
		"arrayField": []interface{}{
			"${array[0]}",
			map[string]interface{}{
				"objectInArray": "${object.in.array}",
			},
		},
	}

	stringFieldSchema := &jsonschema.Schema{
		Types: stv6jsonschema.TypeForSchema("string"),
	}

	objectFieldSchema := &jsonschema.Schema{
		Types: stv6jsonschema.TypeForSchema("object"),
		Properties: map[string]*jsonschema.Schema{
			"key1": {Types: stv6jsonschema.TypeForSchema("string")},
			"key2": {Types: stv6jsonschema.TypeForSchema("integer")},
		},
	}

	nestedObjectFieldSchema := &jsonschema.Schema{
		Types: stv6jsonschema.TypeForSchema("object"),
		Properties: map[string]*jsonschema.Schema{
			"nestedString": {Types: stv6jsonschema.TypeForSchema("string")},
			"nestedObject": {
				Types: stv6jsonschema.TypeForSchema("object"),
				Properties: map[string]*jsonschema.Schema{
					"deepNested": {Types: stv6jsonschema.TypeForSchema("string")},
				},
			},
		},
	}

	arrayFieldItemSchema := &jsonschema.Schema{
		Types: stv6jsonschema.TypeForSchema("object"),
		Properties: map[string]*jsonschema.Schema{
			"objectInArray": {Types: stv6jsonschema.TypeForSchema("string")},
		},
		AdditionalProperties: &jsonschema.Schema{
			Types: stv6jsonschema.TypeForSchema("string"),
		},
	}

	arrayFieldSchema := &jsonschema.Schema{
		Types:     stv6jsonschema.TypeForSchema("array"),
		Items2020: arrayFieldItemSchema,
	}

	schema := &jsonschema.Schema{
		Types: stv6jsonschema.TypeForSchema("object"),
		Properties: map[string]*jsonschema.Schema{
			"stringField":       stringFieldSchema,
			"objectField":       objectFieldSchema,
			"nestedObjectField": nestedObjectFieldSchema,
			"arrayField":        arrayFieldSchema,
		},
	}

	got, err := stv6jsonschema.ParseResource(resource, schema)
	require.NoError(t, err)

	expected := []variable.FieldDescriptor{
		{
			Path:                 fieldpath.MustParse("stringField"),
			Expressions:          []variable.Expression{{Value: "string.value"}},
			StandaloneExpression: true,
		},
		{
			Path:                 fieldpath.MustParse("objectField"),
			Expressions:          []variable.Expression{{Value: "object.value"}},
			StandaloneExpression: true,
		},
		{
			Path:                 fieldpath.MustParse("nestedObjectField.nestedString"),
			Expressions:          []variable.Expression{{Value: "nested.string"}},
			StandaloneExpression: true,
		},
		{
			Path:                 fieldpath.MustParse("nestedObjectField.nestedObject.deepNested"),
			Expressions:          []variable.Expression{{Value: "deep.nested"}},
			StandaloneExpression: true,
		},
		{
			Path:                 fieldpath.MustParse("arrayField[0]"),
			Expressions:          []variable.Expression{{Value: "array[0]"}},
			StandaloneExpression: true,
		},
		{
			Path:                 fieldpath.MustParse("arrayField[1].objectInArray"),
			Expressions:          []variable.Expression{{Value: "object.in.array"}},
			StandaloneExpression: true,
		},
	}

	// Sort both slices for consistent ordering
	sortByPath := func(a, b variable.FieldDescriptor) int {
		return fieldpath.Compare(a.Path, b.Path)
	}
	slices.SortFunc(got, sortByPath)
	slices.SortFunc(expected, sortByPath)

	require.Equal(t, len(expected), len(got), "unexpected number of expressions")

	for i := range expected {
		exp := expected[i]
		act := got[i]

		assert.True(t, act.Path.Equals(exp.Path),
			"path mismatch:\n  got: %s\n want: %s", act.Path, exp.Path)

		require.Equal(t, len(exp.Expressions), len(act.Expressions),
			"expression count mismatch at path %s", exp.Path)

		for j := range exp.Expressions {
			assert.Equal(t, exp.Expressions[j].Value, act.Expressions[j].Value,
				"expression[%d] mismatch at path %s", j, exp.Path)
		}

		assert.Equal(t, exp.StandaloneExpression, act.StandaloneExpression,
			"StandaloneExpression mismatch at %s", exp.Path)
	}
}

func TestParserEdgeCases(t *testing.T) {
	testCases := []struct {
		name          string
		schema        *jsonschema.Schema
		resource      map[string]any
		expectedError string
	}{
		{
			name: "Type mismatch: array expected, got object",
			schema: &jsonschema.Schema{
				Types: stv6jsonschema.TypeForSchema("array"),
				Items2020: &jsonschema.Schema{
					Types: stv6jsonschema.TypeForSchema("string"),
				},
			},
			resource:      map[string]interface{}{"key": "value"},
			expectedError: "jsonschema validation failed with ''\n- at '': got object, want array",
		},
		{
			name: "Unknown property in object (allowed by default in json schema)",
			schema: &jsonschema.Schema{
				Types: stv6jsonschema.TypeForSchema("object"),
				Properties: map[string]*jsonschema.Schema{
					"name": {Types: stv6jsonschema.TypeForSchema("string")},
					"age":  {Types: stv6jsonschema.TypeForSchema("integer")},
				},
			},
			resource: map[string]interface{}{
				"name":    "parrot",
				"surname": "unknown",
			},
		},
		{
			name: "valid schema and resource (no error expected)",
			schema: &jsonschema.Schema{
				Types: stv6jsonschema.TypeForSchema("object"),
				Properties: map[string]*jsonschema.Schema{
					"name": {Types: stv6jsonschema.TypeForSchema("string")},
					"age":  {Types: stv6jsonschema.TypeForSchema("integer")},
				},
			},
			resource: map[string]interface{}{
				"name": "John",
				"age":  30,
			},
			expectedError: "",
		},
		{
			name: "schema with x-kubernetes-preserve-unknown-fields (top-level passthrough)",
			schema: &jsonschema.Schema{
				Types: stv6jsonschema.TypeForSchema("object"),
				AdditionalProperties: &jsonschema.Schema{
					Types: stv6jsonschema.TypeForSchema("object"),
				},
			},
			resource: map[string]interface{}{
				"name":  "John",
				"extra": map[string]interface{}{"nested": "${expr.value}"},
			},
			expectedError: "jsonschema validation failed with ''\n- at '/name': got string, want object",
		},
		{
			name: "structured object with metadata",
			schema: &jsonschema.Schema{
				Types: stv6jsonschema.TypeForSchema("object"),
				Properties: map[string]*jsonschema.Schema{
					"id": {Types: stv6jsonschema.TypeForSchema("string")},
					"metadata": {
						Types: stv6jsonschema.TypeForSchema("object"),
						AdditionalProperties: &jsonschema.Schema{
							Types: stv6jsonschema.TypeForSchema("string"),
						},
					},
				},
			},
			resource: map[string]interface{}{
				"id": "123",
				"metadata": map[string]interface{}{
					"name": "John",
					"age":  30,
					"test": "${test.value}",
				},
			},
			expectedError: "jsonschema validation failed with ''\n- at '/metadata/age': got number, want string",
		},
		{
			name: "invalid schema: missing type and no OneOf/AnyOf/AdditionalProperties",
			schema: &jsonschema.Schema{
				Properties: map[string]*jsonschema.Schema{
					"name": {},
				},
			},
			resource: map[string]interface{}{
				"name": "John",
			},
			expectedError: "cannot create type information from schema, unsupported schema structure",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := stv6jsonschema.ParseResource(tc.resource, tc.schema)

			if tc.expectedError == "" {
				require.NoError(t, err, "did not expect an error")
				return
			}

			require.Error(t, err, "expected an error but got none")
			assert.ErrorContains(t, err, tc.expectedError)
		})
	}
}

func TestCelExpressionAgainstObjectSchemaDoesNotError(t *testing.T) {
	resource := map[string]any{
		"objField": "${myObject.nested.value}",
	}

	schema := &jsonschema.Schema{
		Types: stv6jsonschema.TypeForSchema("object"),
		Properties: map[string]*jsonschema.Schema{
			"objField": {
				Types: stv6jsonschema.TypeForSchema("object"),
				Properties: map[string]*jsonschema.Schema{
					"nested": {Types: stv6jsonschema.TypeForSchema("string")},
				},
			},
		},
	}

	got, err := stv6jsonschema.ParseResource(resource, schema)
	require.NoError(t, err, "CEL expression should skip JSON Schema validation")

	require.Len(t, got, 1)
	assert.Equal(t,
		fieldpath.MustParse("objField"),
		got[0].Path,
	)

	assert.Equal(t, "myObject.nested.value", got[0].Expressions[0].Value)
	assert.True(t, got[0].StandaloneExpression)

	expectedType := got[0].ExpectedType
	assert.NotNil(t, expectedType)
	assert.Equal(t, expectedType.Kind(), types.StructKind)
}

func TestArrayExpressionPaths(t *testing.T) {
	testCases := []struct {
		name     string
		resource map[string]any
		schema   *jsonschema.Schema
		expected []string
	}{
		{
			name: "simple array expressions",
			resource: map[string]any{
				"arr": []any{
					"${a[0]}",
					"${b.value}",
				},
			},
			schema: &jsonschema.Schema{
				Types: stv6jsonschema.TypeForSchema("object"),
				Properties: map[string]*jsonschema.Schema{
					"arr": {
						Types: stv6jsonschema.TypeForSchema("array"),
						Items2020: &jsonschema.Schema{
							Types: stv6jsonschema.TypeForSchema("string"),
						},
					},
				},
			},
			expected: []string{
				"arr[0]",
				"arr[1]",
			},
		},
		{
			name: "object containing array with expressions",
			resource: map[string]any{
				"arr": []any{
					"${x[0]}",
					map[string]any{
						"nested": "${y[1]}",
					},
				},
			},
			schema: &jsonschema.Schema{
				Types: stv6jsonschema.TypeForSchema("object"),
				Properties: map[string]*jsonschema.Schema{
					"arr": {
						Types: stv6jsonschema.TypeForSchema("array"),
						Items2020: &jsonschema.Schema{
							Types: stv6jsonschema.TypeForSchema("object"),
							Properties: map[string]*jsonschema.Schema{
								"nested": {Types: stv6jsonschema.TypeForSchema("string")},
							},
							AdditionalProperties: &jsonschema.Schema{
								Types: stv6jsonschema.TypeForSchema("string"),
							},
						},
					},
				},
			},
			expected: []string{
				"arr[0]",
				"arr[1].nested",
			},
		},
		{
			name: "deep nested arrays",
			resource: map[string]any{
				"root": []any{
					[]any{
						"${deep[0]}",
					},
				},
			},
			schema: &jsonschema.Schema{
				Types: stv6jsonschema.TypeForSchema("object"),
				Properties: map[string]*jsonschema.Schema{
					"root": {
						Types: stv6jsonschema.TypeForSchema("array"),
						Items2020: &jsonschema.Schema{
							Types: stv6jsonschema.TypeForSchema("array"),
							Items2020: &jsonschema.Schema{
								Types: stv6jsonschema.TypeForSchema("string"),
							},
						},
					},
				},
			},
			expected: []string{
				"root[0][0]",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := stv6jsonschema.ParseResource(tc.resource, tc.schema)
			require.NoError(t, err)

			// Sort paths for stable comparison
			paths := make([]string, len(got))
			for i, d := range got {
				paths[i] = d.Path.String()
			}
			slices.Sort(paths)
			slices.Sort(tc.expected)

			require.Equal(t, len(tc.expected), len(paths), "unexpected number of expression paths")
			assert.Equal(t, tc.expected, paths)
		})
	}
}
