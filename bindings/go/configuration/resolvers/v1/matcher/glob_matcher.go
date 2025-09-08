package matcher

import (
	"fmt"
	"path"
)

type globComponentMatcher struct {
	pattern string
}

func newGlobComponentMatcher(pattern string) (ComponentMatcher, error) {
	// Validate the pattern by attempting to match it against an empty string.
	// If the pattern is invalid, path.Match will return an error.
	if _, err := path.Match(pattern, ""); err != nil {
		return nil, fmt.Errorf("invalid glob pattern '%s': %w", pattern, err)
	}

	return &globComponentMatcher{
		pattern: pattern,
	}, nil
}

func (m *globComponentMatcher) Match(componentName string) bool {
	matched, err := path.Match(m.pattern, componentName)
	if err != nil {
		// According to the docs, the only possible error is ErrBadPattern,
		// which we should have caught during creation.
		// Therefore, we can treat this as a panic.
		panic(fmt.Sprintf("unexpected error during path matching: %s", err))
	}
	return matched
}
