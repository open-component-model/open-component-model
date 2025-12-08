package variable

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFieldAddDependencies(t *testing.T) {
	tests := []struct {
		name              string
		initialDeps       []string
		depsToAdd         []string
		expectedFinalDeps []string
	}{
		{
			name:              "add new dependencies",
			initialDeps:       []string{"resource1", "resource2"},
			depsToAdd:         []string{"resource3", "resource4"},
			expectedFinalDeps: []string{"resource1", "resource2", "resource3", "resource4"},
		},
		{
			name:              "add duplicate dependencies",
			initialDeps:       []string{"resource1", "resource2"},
			depsToAdd:         []string{"resource2", "resource3"},
			expectedFinalDeps: []string{"resource1", "resource2", "resource3"},
		},
		{
			name:              "add to empty dependencies",
			initialDeps:       []string{},
			depsToAdd:         []string{"resource1", "resource2"},
			expectedFinalDeps: []string{"resource1", "resource2"},
		},
		{
			name:              "add empty dependencies",
			initialDeps:       []string{"resource1", "resource2"},
			depsToAdd:         []string{},
			expectedFinalDeps: []string{"resource1", "resource2"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rf := Variable{
				Dependencies: tc.initialDeps,
			}

			rf.AddDependencies(tc.depsToAdd...)

			assert.ElementsMatch(t, tc.expectedFinalDeps, rf.Dependencies)

			seen := make(map[string]bool)
			for _, dep := range rf.Dependencies {
				assert.False(t, seen[dep], "Duplicate dependency found: %s", dep)
				seen[dep] = true
			}
		})
	}
}
