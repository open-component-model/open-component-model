package jsonschema_test

import (
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
	stv6jsonschema "ocm.software/open-component-model/bindings/go/cel/jsonschema/santhosh-tekuri/v6"
)

func TestInferFromGoValue(t *testing.T) {
	tests := []struct {
		name      string
		input     interface{}
		expectErr bool
		validate  func(t *testing.T, s *jsonschema.Schema)
	}{
		{
			name:  "boolean",
			input: true,
			validate: func(t *testing.T, s *jsonschema.Schema) {
				require.Contains(t, s.Types.ToStrings(), "boolean")
				require.NotNil(t, s.Const)
				require.Equal(t, true, *s.Const)
			},
		},
		{
			name:  "integer_int64",
			input: int64(42),
			validate: func(t *testing.T, s *jsonschema.Schema) {
				require.Contains(t, s.Types.ToStrings(), "integer")
				require.Equal(t, int64(42), *s.Const)
			},
		},
		{
			name:  "integer_uint64",
			input: uint64(123),
			validate: func(t *testing.T, s *jsonschema.Schema) {
				require.Contains(t, s.Types.ToStrings(), "integer")
				require.Equal(t, uint64(123), *s.Const)
			},
		},
		{
			name:  "number_float64",
			input: 3.14,
			validate: func(t *testing.T, s *jsonschema.Schema) {
				require.Contains(t, s.Types.ToStrings(), "number")
				require.Equal(t, 3.14, *s.Const)
			},
		},
		{
			name:  "string",
			input: "hello",
			validate: func(t *testing.T, s *jsonschema.Schema) {
				require.Contains(t, s.Types.ToStrings(), "string")
				require.Equal(t, "hello", *s.Const)
			},
		},
		{
			name:  "empty_array",
			input: []interface{}{},
			validate: func(t *testing.T, s *jsonschema.Schema) {
				require.Contains(t, s.Types.ToStrings(), "array")
				require.Nil(t, s.Items2020, "empty array should not infer items schema")
			},
		},
		{
			name:  "array_with_single_type",
			input: []interface{}{int64(5)},
			validate: func(t *testing.T, s *jsonschema.Schema) {
				require.Contains(t, s.Types.ToStrings(), "array")
				require.NotNil(t, s.Items2020)
				require.Contains(t, s.Items2020.Types.ToStrings(), "integer")
				require.Equal(t, int64(5), *s.Items2020.Const)
			},
		},
		{
			name: "nested_array",
			input: []interface{}{
				[]interface{}{"a"},
			},
			validate: func(t *testing.T, s *jsonschema.Schema) {
				require.Contains(t, s.Types.ToStrings(), "array")
				require.NotNil(t, s.Items2020)

				items := s.Items2020
				require.Contains(t, items.Types.ToStrings(), "array")
				require.Contains(t, items.Items2020.Types.ToStrings(), "string")
			},
		},
		{
			name: "object_simple",
			input: map[string]interface{}{
				"name": "bob",
				"age":  int64(30),
			},
			validate: func(t *testing.T, s *jsonschema.Schema) {
				require.Contains(t, s.Types.ToStrings(), "object")

				require.Contains(t, s.Properties, "name")
				require.Contains(t, s.Properties["name"].Types.ToStrings(), "string")

				require.Contains(t, s.Properties, "age")
				require.Contains(t, s.Properties["age"].Types.ToStrings(), "integer")
			},
		},
		{
			name: "object_nested",
			input: map[string]interface{}{
				"config": map[string]interface{}{
					"enabled": true,
					"limits":  []interface{}{1.0},
				},
			},
			validate: func(t *testing.T, s *jsonschema.Schema) {
				require.Contains(t, s.Types.ToStrings(), "object")

				cfg := s.Properties["config"]
				require.Contains(t, cfg.Types.ToStrings(), "object")

				require.Contains(t, cfg.Properties["enabled"].Types.ToStrings(), "boolean")

				limits := cfg.Properties["limits"]
				require.Contains(t, limits.Types.ToStrings(), "array")
				require.Contains(t, limits.Items2020.Types.ToStrings(), "number")
				require.Equal(t, 1.0, *limits.Items2020.Const)
			},
		},
		{
			name:      "unsupported_type",
			input:     struct{}{},
			expectErr: true,
		},
		{
			name:      "nil_is_unsupported",
			input:     nil,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			sch, err := stv6jsonschema.InferFromGoValue(tt.input)

			if tt.expectErr {
				require.Error(t, err)
				require.Nil(t, sch)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, sch)
			tt.validate(t, sch)
		})
	}
}
