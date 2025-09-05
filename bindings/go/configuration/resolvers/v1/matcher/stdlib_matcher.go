package matcher

import (
	"fmt"

	"github.com/gobwas/glob"
)

type sdtLibMatcher struct {
	glob glob.Glob
}

func newSdtLibMatcher(pattern string) (*sdtLibMatcher, error) {
	g, err := glob.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to compile glob pattern %q: %w", pattern, err)
	}

	return &sdtLibMatcher{
		glob: g,
	}, nil
}

func (m *sdtLibMatcher) Match(componentName string) bool {
	if m.glob == nil {
		return false
	}

	return m.glob.Match(componentName)
}
