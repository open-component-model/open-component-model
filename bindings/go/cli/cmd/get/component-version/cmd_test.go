package componentversion

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// TestExtraIdentity verifies that the name attribute is removed from the
// IDENTITY column, because the name already has its own column. Elements without
// an extra identity must yield an empty cell rather than a redundant one.
func TestExtraIdentity(t *testing.T) {
	tests := []struct {
		name     string
		identity runtime.Identity
		expected string
	}{
		{
			name:     "empty identity",
			identity: runtime.Identity{},
			expected: "",
		},
		{
			name:     "name only",
			identity: runtime.Identity{"name": "umc-server"},
			expected: "",
		},
		{
			name:     "name and version",
			identity: runtime.Identity{"name": "umc-server", "version": "3.0"},
			expected: "version=3.0",
		},
		{
			name:     "name and extra identity",
			identity: runtime.Identity{"name": "umc-server", "variant": "legacy"},
			expected: "variant=legacy",
		},
		{
			name:     "extra identity is sorted and keeps all attributes",
			identity: runtime.Identity{"name": "umc-server", "variant": "legacy", "version": "3.0"},
			expected: "variant=legacy,version=3.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			r.Equal(tt.expected, extraIdentity(tt.identity))
		})
	}
}
