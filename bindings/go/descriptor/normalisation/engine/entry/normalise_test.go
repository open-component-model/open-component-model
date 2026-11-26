package entry_test

import (
	// jsonv1 is imported for json.Number only; encoding/json/v2 has no
	// replacement for it (see the engine's number handling).
	jsonv1 "encoding/json"
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
		{
			name:     "integral float keeps encoding/json rendering",
			input:    map[string]any{"k": 1.0},
			expected: `[{"k":1}]`,
		},
		{
			name:     "fractional floats use Python repr spelling",
			input:    []any{0.0000001, 0.00001, 0.0001, 1.25, 0.5},
			expected: `[1e-07,1e-05,0.0001,1.25,0.5]`,
		},
		{
			// json.Number is what the json/v1 algorithm feeds in (UseNumber).
			// Integer tokens are emitted verbatim at full precision and
			// fractional tokens re-spelled to Python's form. Both DIVERGE from
			// OCM v1 (float64): OCM v1 would emit 12345678901234567000 and
			// 0.00001 respectively.
			name:     "json.Number: big int verbatim, fractional re-spelled",
			input:    []any{jsonv1.Number("12345678901234567890"), jsonv1.Number("0.00001"), jsonv1.Number("42")},
			expected: `[12345678901234567890,1e-05,42]`,
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

// TestNormaliseGoJSONMatchesEncodingJSONv1 pins NormaliseGoJSON to the scalar
// spelling of Go's encoding/json (v1) json.Marshal, which the OCM v1 CLI signs
// with.
func TestNormaliseGoJSONMatchesEncodingJSONv1(t *testing.T) {
	tests := []struct {
		value any
		want  string
	}{
		{"plain", `"plain"`},
		{"a<b>&c", `"a\u003cb\u003e\u0026c"`},
		{"line\u2028sep", `"line\u2028sep"`},
		{"para\u2029sep", `"para\u2029sep"`},
		{"ümlaut", "\"ümlaut\""},
		{"tab\tquote\"", `"tab\tquote\""`},
		{0.00001, `0.00001`},
		{1e-7, `1e-7`},
		{1e21, `1e+21`},
		{1.5, `1.5`},
		{9007199254740993.0, `9007199254740992`},
		{-1.0, `-1`},
		{0.0, `0`},
	}
	for _, tt := range tests {
		got, err := entry.NormaliseGoJSON(map[string]any{"v": tt.value})
		require.NoError(t, err)
		require.Equal(t, `[{"v":`+tt.want+`}]`, string(got), "value %#v", tt.value)
	}
}
