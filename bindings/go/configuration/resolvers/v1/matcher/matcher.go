package matcher

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// ComponentMatcher provides functionality to match component names using glob patterns only.
type ComponentMatcher struct {
	pattern string
	regex   *regexp.Regexp
}

// NewComponentMatcher creates a new ComponentMatcher for the given glob pattern.
// All patterns are treated as glob patterns and converted to regex internally.
func NewComponentMatcher(pattern string) (*ComponentMatcher, error) {
	if pattern == "" {
		return &ComponentMatcher{pattern: pattern}, nil
	}

	// Convert glob pattern to regex
	regexPattern := globToRegex(pattern)
	regex, err := regexp.Compile(regexPattern)
	if err != nil {
		return nil, fmt.Errorf("failed to compile glob pattern %q as regex: %w", pattern, err)
	}

	return &ComponentMatcher{
		pattern: pattern,
		regex:   regex,
	}, nil
}

func (m *ComponentMatcher) Match(componentName string) bool {
	if m.pattern == "" {
		return true // Empty pattern matches everything
	}

	if m.regex == nil {
		return false
	}

	return m.regex.MatchString(componentName)
}

func (m *ComponentMatcher) Pattern() string {
	return m.pattern
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

type VersionMatcher struct {
	constraint       string
	semverConstraint *semver.Constraints
}

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
		return true
	}

	if m.semverConstraint == nil {
		return false
	}

	semverVersion, err := semver.NewVersion(version)
	if err != nil {
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

// NewResolverMatcher creates a new ResolverMatcher with the given component name glob pattern and version constraint.
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

func (m *ResolverMatcher) Match(componentName, version string) bool {
	return m.componentMatcher.Match(componentName) && m.versionMatcher.Match(version)
}

func (m *ResolverMatcher) MatchComponent(componentName string) bool {
	return m.componentMatcher.Match(componentName)
}

func (m *ResolverMatcher) MatchVersion(version string) bool {
	return m.versionMatcher.Match(version)
}

func (m *ResolverMatcher) ComponentPattern() string {
	return m.componentMatcher.Pattern()
}

func (m *ResolverMatcher) VersionConstraint() string {
	return m.versionMatcher.Constraint()
}
