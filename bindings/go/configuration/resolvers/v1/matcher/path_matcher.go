package matcher

import (
	"fmt"
	"path"
)

type pathComponentMatcher struct {
	pattern string
}

func newPathComponentMatcher(pattern string) (ComponentMatcher, error) {
	return &pathComponentMatcher{
		pattern: pattern,
	}, nil
}

func (m *pathComponentMatcher) Match(componentName string) bool {
	matched, err := path.Match(m.pattern, componentName)
	if err != nil {
		// According to the docs, the only possible error is ErrBadPattern,
		// which we should have caught during creation.
		// Therefore, we can treat this as a panic.
		panic(fmt.Sprintf("unexpected error during path matching: %s", err))
	}
	return matched
}
