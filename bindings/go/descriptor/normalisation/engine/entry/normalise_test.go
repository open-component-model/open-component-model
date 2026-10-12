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
