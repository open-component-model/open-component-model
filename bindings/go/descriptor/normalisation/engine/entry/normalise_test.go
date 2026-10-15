package entry_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/engine/entry"
)

func TestNormalise(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{
			name:     "scalar string",
			input:    "x",
			expected: `"x"`,
		},
		{
			name:     "explicit null preserved",
			input:    map[string]any{"extraIdentity": nil},
			expected: `[{"extraIdentity":null}]`,
		},
		{
			name:     "object becomes sorted single-key array",
			input:    map[string]any{"b": "2", "a": "1"},
			expected: `[{"a":"1"},{"b":"2"}]`,
		},
		{
			name:     "empty array stays empty array",
			input:    map[string]any{"componentReferences": []any{}},
			expected: `[{"componentReferences":[]}]`,
		},
		{
			name: "nested object and array",
			input: map[string]any{
				"digest": map[string]any{
					"value":         "aa",
					"hashAlgorithm": "SHA-256",
				},
			},
			expected: `[{"digest":[{"hashAlgorithm":"SHA-256"},{"value":"aa"}]}]`,
		},
		{
			name: "array elements are left raw, not recursed",
			input: map[string]any{
				"value": []any{map[string]any{"b": 1.0, "a": 2.0}},
			},
			expected: `[{"value":[{"a":2,"b":1}]}]`,
		},
		{
			name:     "non-ASCII escaped to \\uXXXX",
			input:    map[string]any{"k": "café"},
			expected: `[{"k":"caf\u00e9"}]`,
		},
		{
			name:     "no HTML escaping",
			input:    map[string]any{"k": "a<b>&c"},
			expected: `[{"k":"a<b>&c"}]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			got, err := entry.Normalise(tt.input)
			r.NoError(err)
			r.Equal(tt.expected, string(got))
		})
	}
}

// TestTransformRaw locks the raw-JSON path: objects become sorted single-key
// arrays, while arrays (and objects nested inside them) are emitted raw with
// source key order preserved — mirroring cc-utils' normalise_obj.
func TestTransformRaw(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "object keys sorted into single-key arrays",
			input:    `{"b":"2","a":"1"}`,
			expected: `[{"a":"1"},{"b":"2"}]`,
		},
		{
			name:     "array object keys kept in source order",
			input:    `[{"b":1,"a":2}]`,
			expected: `[{"b":1,"a":2}]`,
		},
		{
			name:     "dict value recursed+sorted, nested list kept raw",
			input:    `{"z":[{"y":1,"x":2}],"a":"first"}`,
			expected: `[{"a":"first"},{"z":[{"y":1,"x":2}]}]`,
		},
		{
			name:     "escaping normalised, non-ASCII to \\uXXXX",
			input:    `[{"k":"caf\u00e9 <&>"}]`,
			expected: `[{"k":"caf\u00e9 <&>"}]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			v, err := entry.TransformRaw([]byte(tt.input))
			r.NoError(err)
			got, err := entry.Normalise(v)
			r.NoError(err)
			r.Equal(tt.expected, string(got))
		})
	}
}
