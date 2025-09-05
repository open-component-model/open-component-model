package matcher

import (
	"fmt"
	"strings"
)

type ComponentMatcher interface {
	Match(componentName string) bool
}

func NewComponentMatcher(pattern string) (ComponentMatcher, error) {
	if strings.HasPrefix(pattern, "path:") {
		return newPathComponentMatcher(strings.TrimPrefix(pattern, "path:"))
	}
	return newGlobComponentMatcher(pattern)
}

// ResolverMatcher combines component name and version matching for a resolver.
type ResolverMatcher struct {
	componentMatcher ComponentMatcher
}

// NewResolverMatcher creates a new ResolverMatcher with the given component name glob pattern and version constraint.
func NewResolverMatcher(componentNamePattern string) (*ResolverMatcher, error) {
	componentMatcher, err := NewComponentMatcher(componentNamePattern)
	if err != nil {
		return nil, fmt.Errorf("failed to create component matcher: %w", err)
	}

	return &ResolverMatcher{
		componentMatcher: componentMatcher,
	}, nil
}

func (m *ResolverMatcher) Match(componentName, version string) bool {
	return m.componentMatcher.Match(componentName)
}

func (m *ResolverMatcher) MatchComponent(componentName string) bool {
	return m.componentMatcher.Match(componentName)
}
