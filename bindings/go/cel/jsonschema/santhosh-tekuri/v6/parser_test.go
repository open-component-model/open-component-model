// Copyright 2025 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package jsonschema_test

import (
	"slices"
	"testing"

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
				"":                     "${emptyannotation}",
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
						"":                     {Types: stv6jsonschema.TypeForSchema("string")},
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
			{Path: fieldpath.MustParse(`specialCharacters[""]`), Expressions: []variable.Expression{{Value: "emptyannotation"}}, StandaloneExpression: true},
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
			expectedError: "expected integer type for path intField, got string",
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
			expectedError: "expected string type for path stringField, got integer",
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
			expectedError: "expected number type for path numberField, got boolean",
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
			expectedError: "expected object type for path objectField, got array",
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
			expectedError: "expected array type for path \"arrayField\" to be object or any",
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
			expectedError: "expected number type for path level1.level2.numberField, got string",
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
			expectedError: "schema at path \"\" has no valid type, OneOf, AnyOf, or AdditionalProperties",
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
					assert.Equal(t, tc.expectedError, err.Error())
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
			expectedError: "expected array type for path \"\" to be object or any",
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
			expectedError: "expected object type for path name, got string",
		},
		{
			name: "structured object with nested x-kubernetes-preserve-unknown-fields",
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
			expectedError: "expected string type for path metadata.age, got integer",
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
			expectedError: "schema at path \"\" has no valid type, OneOf, AnyOf, or AdditionalProperties",
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
			assert.Equal(t, tc.expectedError, err.Error())
		})
	}
}
