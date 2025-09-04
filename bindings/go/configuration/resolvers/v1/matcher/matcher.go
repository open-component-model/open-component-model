package matcher

import (
	"fmt"

	"github.com/gobwas/glob"
)

type ComponentMatcher interface {
	Match(componentName string) bool
}

type globComponentMatcher struct {
	glob glob.Glob
}

func NewComponentMatcher(pattern string) (ComponentMatcher, error) {
	g, err := glob.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to compile glob pattern %q: %w", pattern, err)
	}

	return &globComponentMatcher{
		glob: g,
	}, nil
}

func (m *globComponentMatcher) Match(componentName string) bool {
	if m.glob == nil {
		return false
	}

	return m.glob.Match(componentName)
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
