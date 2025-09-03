package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComponentMatcher(t *testing.T) {
	tests := []struct {
		name          string
		pattern       string
		componentName string
		shouldMatch   bool
		expectError   bool
	}{
		{
			name:          "empty pattern matches everything",
			pattern:       "",
			componentName: "ocm.software/core/test",
			shouldMatch:   true,
		},
		{
			name:          "glob pattern with wildcard",
			pattern:       "ocm.software/core/*",
			componentName: "ocm.software/core/test",
			shouldMatch:   true,
		},
		{
			name:          "glob pattern no match",
			pattern:       "ocm.software/core/*",
			componentName: "ocm.software/other/test",
			shouldMatch:   false,
		},
		{
			name:          "regex pattern with anchors",
			pattern:       "^ocm\\.software/.*$",
			componentName: "ocm.software/core/test",
			shouldMatch:   true,
		},
		{
			name:          "regex pattern no match",
			pattern:       "ocm\\.software/core/.*",
			componentName: "ocm.software/other/test",
			shouldMatch:   false,
		},
		{
			name:          "exact match",
			pattern:       "ocm.software/core/test",
			componentName: "ocm.software/core/test",
			shouldMatch:   true,
		},
		{
			name:          "exact no match",
			pattern:       "ocm.software/core/test",
			componentName: "ocm.software/core/other",
			shouldMatch:   false,
		},
		{
			name:        "invalid regex",
			pattern:     "[invalid",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher, err := NewComponentMatcher(tt.pattern)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.shouldMatch, matcher.Match(tt.componentName))
			assert.Equal(t, tt.pattern, matcher.Pattern())
		})
	}
}

func TestVersionMatcher(t *testing.T) {
	tests := []struct {
		name        string
		constraint  string
		version     string
		shouldMatch bool
		expectError bool
	}{
		{
			name:        "empty constraint matches everything",
			constraint:  "",
			version:     "1.2.3",
			shouldMatch: true,
		},
		{
			name:        "greater than constraint match",
			constraint:  ">1.0.0",
			version:     "1.2.3",
			shouldMatch: true,
		},
		{
			name:        "greater than constraint no match",
			constraint:  ">1.0.0",
			version:     "0.9.0",
			shouldMatch: false,
		},
		{
			name:        "range constraint match",
			constraint:  ">=1.0.0 <2.0.0",
			version:     "1.5.0",
			shouldMatch: true,
		},
		{
			name:        "range constraint no match",
			constraint:  ">=1.0.0 <2.0.0",
			version:     "2.1.0",
			shouldMatch: false,
		},
		{
			name:        "tilde constraint match",
			constraint:  "~1.2.3",
			version:     "1.2.5",
			shouldMatch: true,
		},
		{
			name:        "tilde constraint no match",
			constraint:  "~1.2.3",
			version:     "1.3.0",
			shouldMatch: false,
		},
		{
			name:        "caret constraint match",
			constraint:  "^1.2.3",
			version:     "1.5.0",
			shouldMatch: true,
		},
		{
			name:        "caret constraint no match",
			constraint:  "^1.2.3",
			version:     "2.0.0",
			shouldMatch: false,
		},
		{
			name:        "invalid version",
			constraint:  ">1.0.0",
			version:     "not-a-version",
			shouldMatch: false,
		},
		{
			name:        "invalid constraint",
			constraint:  "invalid-constraint",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher, err := NewVersionMatcher(tt.constraint)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.shouldMatch, matcher.Match(tt.version))
			assert.Equal(t, tt.constraint, matcher.Constraint())
		})
	}
}

func TestResolverMatcher(t *testing.T) {
	tests := []struct {
		name                 string
		componentNamePattern string
		versionConstraint    string
		componentName        string
		version              string
		shouldMatch          bool
		expectError          bool
	}{
		{
			name:                 "both match",
			componentNamePattern: "ocm.software/*",
			versionConstraint:    ">1.0.0",
			componentName:        "ocm.software/core",
			version:              "1.2.3",
			shouldMatch:          true,
		},
		{
			name:                 "component matches, version doesn't",
			componentNamePattern: "ocm.software/*",
			versionConstraint:    ">1.0.0",
			componentName:        "ocm.software/core",
			version:              "0.9.0",
			shouldMatch:          false,
		},
		{
			name:                 "component doesn't match, version does",
			componentNamePattern: "ocm.software/*",
			versionConstraint:    ">1.0.0",
			componentName:        "other.software/core",
			version:              "1.2.3",
			shouldMatch:          false,
		},
		{
			name:                 "neither matches",
			componentNamePattern: "ocm.software/*",
			versionConstraint:    ">1.0.0",
			componentName:        "other.software/core",
			version:              "0.9.0",
			shouldMatch:          false,
		},
		{
			name:                 "empty patterns match everything",
			componentNamePattern: "",
			versionConstraint:    "",
			componentName:        "any.component/name",
			version:              "any-version",
			shouldMatch:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher, err := NewResolverMatcher(tt.componentNamePattern, tt.versionConstraint)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.shouldMatch, matcher.Match(tt.componentName, tt.version))
			assert.Equal(t, tt.shouldMatch, matcher.MatchComponent(tt.componentName) && matcher.MatchVersion(tt.version))
			assert.Equal(t, tt.componentNamePattern, matcher.ComponentPattern())
			assert.Equal(t, tt.versionConstraint, matcher.VersionConstraint())
		})
	}
}

func TestGlobToRegex(t *testing.T) {
	tests := []struct {
		name     string
		glob     string
		expected string
	}{
		{
			name:     "simple wildcard",
			glob:     "test/*",
			expected: "^test/.*$",
		},
		{
			name:     "question mark",
			glob:     "test?",
			expected: "^test.$",
		},
		{
			name:     "character class",
			glob:     "test[abc]",
			expected: "^test[abc]$",
		},
		{
			name:     "escaped special chars",
			glob:     "test.example",
			expected: "^test\\.example$",
		},
		{
			name:     "complex pattern",
			glob:     "ocm.software/*/v?.*",
			expected: "^ocm\\.software/.*/v.\\..*$",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := globToRegex(tt.glob)
			assert.Equal(t, tt.expected, result)
		})
	}
}
