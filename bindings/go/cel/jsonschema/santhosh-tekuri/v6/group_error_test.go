package jsonschema_test

import (
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	stv6jsonschema "ocm.software/open-component-model/bindings/go/cel/jsonschema/santhosh-tekuri/v6"
)

// TestPureGroupErrorSuppression tests specific scenarios that create *kind.Group errors
// (not AllOf/AnyOf/OneOf) and verifies they are properly suppressed with expressions
func TestPureGroupErrorSuppression(t *testing.T) {
	testCases := []struct {
		name      string
		resource  map[string]any
		schema    *jsonschema.Schema
		expectErr bool
		errMsg    string
	}{
		{
			name: "multiple property errors create Group error - should be suppressed with expressions",
			resource: map[string]any{
				"prop1": "${expr1.value}", // Would violate type constraint
				"prop2": "${expr2.value}", // Would violate type constraint
				"prop3": "${expr3.value}", // Would violate type constraint
			},
			schema: &jsonschema.Schema{
				Types: stv6jsonschema.TypeForSchema("object"),
				Properties: map[string]*jsonschema.Schema{
					"prop1": {Types: stv6jsonschema.TypeForSchema("number")},  // Expression is string, expects number
					"prop2": {Types: stv6jsonschema.TypeForSchema("boolean")}, // Expression is string, expects boolean
					"prop3": {Types: stv6jsonschema.TypeForSchema("array")},   // Expression is string, expects array
				},
				Required: []string{"prop1", "prop2", "prop3"},
			},
			expectErr: false, // Group error should be suppressed due to expressions
		},
		{
			name: "multiple property errors without expressions - should NOT be suppressed",
			resource: map[string]any{
				"prop1": "string_value",   // Wrong type, expects number
				"prop2": "another_string", // Wrong type, expects boolean
				"prop3": "third_string",   // Wrong type, expects array
			},
			schema: &jsonschema.Schema{
				Types: stv6jsonschema.TypeForSchema("object"),
				Properties: map[string]*jsonschema.Schema{
					"prop1": {Types: stv6jsonschema.TypeForSchema("number")},
					"prop2": {Types: stv6jsonschema.TypeForSchema("boolean")},
					"prop3": {Types: stv6jsonschema.TypeForSchema("array")},
				},
				Required: []string{"prop1", "prop2", "prop3"},
			},
			expectErr: true, // Real Group error should NOT be suppressed
		},
		{
			name: "array items with multiple errors create Group error - should be suppressed with expressions",
			resource: map[string]any{
				"items": []any{
					"${item1.expr}", // String expression, expects number
					"${item2.expr}", // String expression, expects number
					"${item3.expr}", // String expression, expects number
				},
			},
			schema: &jsonschema.Schema{
				Types: stv6jsonschema.TypeForSchema("object"),
				Properties: map[string]*jsonschema.Schema{
					"items": {
						Types: stv6jsonschema.TypeForSchema("array"),
						Items2020: &jsonschema.Schema{
							Types: stv6jsonschema.TypeForSchema("number"),
						},
					},
				},
			},
			expectErr: false, // Group error should be suppressed due to expressions in array items
		},
		{
			name: "mixed expressions and non-expressions - should be suppressed when expressions present",
			resource: map[string]any{
				"field1": "${expr.value}",   // Expression
				"field2": 42,                // Valid value
				"field3": "${another.expr}", // Expression
			},
			schema: &jsonschema.Schema{
				Types: stv6jsonschema.TypeForSchema("object"),
				Properties: map[string]*jsonschema.Schema{
					"field1": {Types: stv6jsonschema.TypeForSchema("boolean")}, // Expression is string, expects boolean
					"field2": {Types: stv6jsonschema.TypeForSchema("number")},  // Valid
					"field3": {Types: stv6jsonschema.TypeForSchema("array")},   // Expression is string, expects array
				},
			},
			expectErr: false, // Should be suppressed due to expressions present
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := stv6jsonschema.ParseResource(tc.resource, tc.schema)

			if tc.expectErr {
				require.Error(t, err)
				if tc.errMsg != "" {
					assert.Contains(t, err.Error(), tc.errMsg)
				}
			} else {
				require.NoError(t, err, "expected Group error to be suppressed for container with expressions")
			}
		})
	}
}

// TestGroupErrorValidation verifies that we're actually testing Group errors
func TestGroupErrorValidation(t *testing.T) {
	t.Run("verify multiple property errors create Group error", func(t *testing.T) {
		// This test ensures our test scenarios actually create Group errors when expected
		resource := map[string]any{
			"prop1": "string_value",
			"prop2": "another_string",
			"prop3": "third_string",
		}

		schema := &jsonschema.Schema{
			Types: stv6jsonschema.TypeForSchema("object"),
			Properties: map[string]*jsonschema.Schema{
				"prop1": {Types: stv6jsonschema.TypeForSchema("number")},
				"prop2": {Types: stv6jsonschema.TypeForSchema("boolean")},
				"prop3": {Types: stv6jsonschema.TypeForSchema("array")},
			},
			Required: []string{"prop1", "prop2", "prop3"},
		}

		// Direct validation should create Group error
		directErr := schema.Validate(resource)
		require.Error(t, directErr, "direct validation should fail")

		// Check that this actually creates a Group error structure
		if valErr, ok := directErr.(*jsonschema.ValidationError); ok {
			t.Logf("Root error type: %T", valErr.ErrorKind)
			t.Logf("Number of causes: %d", len(valErr.Causes))

			// Should have multiple causes (creating Group scenario)
			assert.Greater(t, len(valErr.Causes), 1, "should have multiple validation errors to create Group scenario")
		}
	})

	t.Run("verify Group vs AllOf vs Schema error types", func(t *testing.T) {
		// Create scenarios that generate different error types to understand the differences

		// Test case 1: Multiple independent property errors (should create Group)
		multiPropResource := map[string]any{
			"a": "wrong_type_for_number",
			"b": "wrong_type_for_boolean",
		}
		multiPropSchema := &jsonschema.Schema{
			Types: stv6jsonschema.TypeForSchema("object"),
			Properties: map[string]*jsonschema.Schema{
				"a": {Types: stv6jsonschema.TypeForSchema("number")},
				"b": {Types: stv6jsonschema.TypeForSchema("boolean")},
			},
		}

		multiPropErr := multiPropSchema.Validate(multiPropResource)
		if multiPropErr != nil {
			if valErr, ok := multiPropErr.(*jsonschema.ValidationError); ok {
				t.Logf("Multiple properties error type: %T", valErr.ErrorKind)
				t.Logf("Multiple properties causes: %d", len(valErr.Causes))
			}
		}

		// Test case 2: AllOf constraint error (should create AllOf)
		allOfResource := map[string]any{
			"field": "test_value",
		}
		allOfSchema := &jsonschema.Schema{
			Types: stv6jsonschema.TypeForSchema("object"),
			Properties: map[string]*jsonschema.Schema{
				"field": {
					AllOf: []*jsonschema.Schema{
						{Types: stv6jsonschema.TypeForSchema("string")},
						{Types: stv6jsonschema.TypeForSchema("number")}, // Conflicting
					},
				},
			},
		}

		allOfErr := allOfSchema.Validate(allOfResource)
		if allOfErr != nil {
			if valErr, ok := allOfErr.(*jsonschema.ValidationError); ok {
				t.Logf("AllOf error type: %T", valErr.ErrorKind)
				t.Logf("AllOf causes: %d", len(valErr.Causes))
				// Walk the error tree to see nested error types
				if len(valErr.Causes) > 0 {
					t.Logf("AllOf first cause type: %T", valErr.Causes[0].ErrorKind)
				}
			}
		}
	})
}
