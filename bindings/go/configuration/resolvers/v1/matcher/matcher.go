package matcher

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// ComponentMatcher provides functionality to match component names using regex or glob patterns.
type ComponentMatcher struct {
	pattern string
	regex   *regexp.Regexp
	isGlob  bool
}

// NewComponentMatcher creates a new ComponentMatcher for the given pattern.
// It automatically detects whether the pattern is a glob or regex pattern.
func NewComponentMatcher(pattern string) (*ComponentMatcher, error) {
	if pattern == "" {
		return &ComponentMatcher{pattern: pattern}, nil
	}

	// Detect if it's a glob pattern (contains unescaped *, ?, or [])
	// We need to be careful not to treat escaped regex patterns as globs
	isGlob := containsUnescapedGlobChars(pattern)

	var regex *regexp.Regexp
	var err error

	if isGlob {
		// Convert glob pattern to regex
		regexPattern := globToRegex(pattern)
		regex, err = regexp.Compile(regexPattern)
		if err != nil {
			return nil, fmt.Errorf("failed to compile glob pattern %q as regex: %w", pattern, err)
		}
	} else {
		// Try to compile as regex first
		regex, err = regexp.Compile(pattern)
		if err != nil {
			// If it fails as regex, try as literal string
			escapedPattern := regexp.QuoteMeta(pattern)
			regex, err = regexp.Compile("^" + escapedPattern + "$")
			if err != nil {
				return nil, fmt.Errorf("failed to compile pattern %q: %w", pattern, err)
			}
		} else {
			// Check if the regex needs anchoring (if it doesn't start with ^ or end with $)
			if !strings.HasPrefix(pattern, "^") && !strings.HasSuffix(pattern, "$") {
				// Re-compile with anchors for full string matching
				anchoredPattern := "^" + pattern + "$"
				regex, err = regexp.Compile(anchoredPattern)
				if err != nil {
					return nil, fmt.Errorf("failed to compile anchored pattern %q: %w", anchoredPattern, err)
				}
			}
		}
	}

	return &ComponentMatcher{
		pattern: pattern,
		regex:   regex,
		isGlob:  isGlob,
	}, nil
}

// Match returns true if the component name matches the pattern.
func (m *ComponentMatcher) Match(componentName string) bool {
	if m.pattern == "" {
		return true // Empty pattern matches everything
	}

	if m.regex == nil {
		return false
	}

	return m.regex.MatchString(componentName)
}

// Pattern returns the original pattern string.
func (m *ComponentMatcher) Pattern() string {
	return m.pattern
}

// IsGlob returns true if the pattern is detected as a glob pattern.
func (m *ComponentMatcher) IsGlob() bool {
	return m.isGlob
}

// containsUnescapedGlobChars checks if the pattern contains unescaped glob characters.
// It tries to distinguish between regex and glob patterns by looking for regex-specific features.
func containsUnescapedGlobChars(pattern string) bool {
	// If the pattern has regex anchors (^ at start or $ at end), treat it as regex
	if strings.HasPrefix(pattern, "^") || strings.HasSuffix(pattern, "$") {
		return false
	}

	// If the pattern contains escaped dots (common in regex), treat it as regex
	if strings.Contains(pattern, "\\.") {
		return false
	}

	// If the pattern contains regex-specific sequences like .* or .+, treat it as regex
	if strings.Contains(pattern, ".*") || strings.Contains(pattern, ".+") {
		return false
	}

	// Now check for unescaped glob characters
	runes := []rune(pattern)
	for i, char := range runes {
		if char == '*' || char == '?' || char == '[' {
			// Check if this character is escaped (preceded by \)
			if i == 0 || runes[i-1] != '\\' {
				return true
			}
		}
	}
	return false
}

// globToRegex converts a glob pattern to a regex pattern.
func globToRegex(glob string) string {
	// Escape regex special characters except for glob wildcards
	regex := regexp.QuoteMeta(glob)

	// Replace escaped glob wildcards with regex equivalents
	regex = strings.ReplaceAll(regex, `\*`, ".*")
	regex = strings.ReplaceAll(regex, `\?`, ".")

	// Handle character classes [abc] -> [abc]
	regex = strings.ReplaceAll(regex, `\[`, "[")
	regex = strings.ReplaceAll(regex, `\]`, "]")

	// Anchor the regex to match the entire string
	return "^" + regex + "$"
}

// VersionMatcher provides functionality to match component versions using semantic version constraints.
type VersionMatcher struct {
	constraint       string
	semverConstraint *semver.Constraints
}

// NewVersionMatcher creates a new VersionMatcher for the given semantic version constraint.
func NewVersionMatcher(constraint string) (*VersionMatcher, error) {
	if constraint == "" {
		return &VersionMatcher{constraint: constraint}, nil
	}

	semverConstraint, err := semver.NewConstraint(constraint)
	if err != nil {
		return nil, fmt.Errorf("failed to parse semantic version constraint %q: %w", constraint, err)
	}

	return &VersionMatcher{
		constraint:       constraint,
		semverConstraint: semverConstraint,
	}, nil
}

// Match returns true if the version satisfies the constraint.
func (m *VersionMatcher) Match(version string) bool {
	if m.constraint == "" {
		return true // Empty constraint matches everything
	}

	if m.semverConstraint == nil {
		return false
	}

	semverVersion, err := semver.NewVersion(version)
	if err != nil {
		// If version is not a valid semantic version, it doesn't match
		return false
	}

	return m.semverConstraint.Check(semverVersion)
}

// Constraint returns the original constraint string.
func (m *VersionMatcher) Constraint() string {
	return m.constraint
}

// ResolverMatcher combines component name and version matching for a resolver.
type ResolverMatcher struct {
	componentMatcher *ComponentMatcher
	versionMatcher   *VersionMatcher
}

// NewResolverMatcher creates a new ResolverMatcher with the given component name pattern and version constraint.
func NewResolverMatcher(componentNamePattern, versionConstraint string) (*ResolverMatcher, error) {
	componentMatcher, err := NewComponentMatcher(componentNamePattern)
	if err != nil {
		return nil, fmt.Errorf("failed to create component matcher: %w", err)
	}

	versionMatcher, err := NewVersionMatcher(versionConstraint)
	if err != nil {
		return nil, fmt.Errorf("failed to create version matcher: %w", err)
	}

	return &ResolverMatcher{
		componentMatcher: componentMatcher,
		versionMatcher:   versionMatcher,
	}, nil
}

// Match returns true if both the component name and version match their respective patterns/constraints.
func (m *ResolverMatcher) Match(componentName, version string) bool {
	return m.componentMatcher.Match(componentName) && m.versionMatcher.Match(version)
}

// MatchComponent returns true if the component name matches the pattern.
func (m *ResolverMatcher) MatchComponent(componentName string) bool {
	return m.componentMatcher.Match(componentName)
}

// MatchVersion returns true if the version matches the constraint.
func (m *ResolverMatcher) MatchVersion(version string) bool {
	return m.versionMatcher.Match(version)
}

// ComponentPattern returns the component name pattern.
func (m *ResolverMatcher) ComponentPattern() string {
	return m.componentMatcher.Pattern()
}

// VersionConstraint returns the version constraint.
func (m *ResolverMatcher) VersionConstraint() string {
	return m.versionMatcher.Constraint()
}
