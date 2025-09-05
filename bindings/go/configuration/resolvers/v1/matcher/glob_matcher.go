package matcher

import (
	"fmt"

	"github.com/gobwas/glob"
)

type globComponentMatcher struct {
	glob glob.Glob
}

func newGlobComponentMatcher(pattern string) (*globComponentMatcher, error) {
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
