package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFloat64Exact(t *testing.T) {
	for token, want := range map[string]bool{
		"0":                   true,
		"-0":                  true,
		"1.5":                 true,
		"0.1":                 true,
		"0.00001":             true,
		"1e-05":               true,
		"1E+16":               true,
		"9007199254740992":    true,
		"9007199254740993":    false,
		"0.10000000000000001": false,
		"1e400":               false,
		"not-a-number":        false,
	} {
		t.Run(token, func(t *testing.T) {
			require.Equal(t, want, float64Exact(token))
		})
	}
}
