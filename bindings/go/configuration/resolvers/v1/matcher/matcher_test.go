package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolverMatcher(t *testing.T) {
	tests := []struct {
		name                 string
		componentNamePattern string
		componentName        string
		shouldMatch          bool
		expectError          bool
	}{
		{
			name:                 "component matches",
			componentNamePattern: "ocm.software/*",
			componentName:        "ocm.software/core",
			shouldMatch:          true,
		},
		{
			name:                 "component doesn't match",
			componentNamePattern: "ocm.software/*",
			componentName:        "other.software/core",
			shouldMatch:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher, err := NewResolverMatcher(tt.componentNamePattern)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.shouldMatch, matcher.Match(tt.componentName, ""))
			assert.Equal(t, tt.shouldMatch, matcher.MatchComponent(tt.componentName))
		})
	}
}
