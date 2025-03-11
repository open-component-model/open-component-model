package v1

import (
	"maps"
	"path"
)

const DefaultMatchingPriority = 100

type IdentityMatchingFn func(Identity, Identity) bool

type IdentityMatcher interface {
	Match(Identity, Identity) bool
}

type functionalIdentityMatcher struct {
	fn IdentityMatchingFn
}

func (f *functionalIdentityMatcher) Match(a, b Identity) bool {
	return f.fn(a, b)
}

func NewMatcher(fn IdentityMatchingFn) IdentityMatcher {
	return &functionalIdentityMatcher{fn: fn}
}

func IdentityEqual(i, o Identity) bool {
	del := func(s string, s2 string) bool {
		return true
	}
	defer func() {
		maps.DeleteFunc(i, del)
		maps.DeleteFunc(o, del)
	}()
	return i.Equal(o)
}

func IdentityIsContained(i, o Identity) bool {
	del := func(s string, s2 string) bool {
		return true
	}
	defer func() {
		maps.DeleteFunc(i, del)
		maps.DeleteFunc(o, del)
	}()
	return i.IsContainedIn(o)
}

// IdentityMatchesPath returns true if the identity a matches the subpath of the identity b.
// If the path attribute is not set in either identity, it returns true.
// If the path attribute is set in both identities,
// it returns true if the path attribute of b contains the path attribute of a.
// For more information, check path.Match
func IdentityMatchesPath(i, o Identity) bool {
	ip, iok := i[IdentityAttributePath]
	delete(i, IdentityAttributePath)
	op, ook := o[IdentityAttributePath]
	delete(o, IdentityAttributePath)
	if !iok && !ook || (ip == "" && op == "") || op == "" {
		return true
	}
	match, err := path.Match(op, ip)
	if err != nil {
		return false
	}
	return match
}

// Match returns true if the identity a matches the identity b.
// It uses the provided matchers to determine the match.
// If no matchers are provided, it uses IdentityMatchesPath and IdentityEqual in order.
// If any matcher returns false, it returns false.
func (i Identity) Match(o Identity, matchers ...IdentityMatcher) bool {
	if len(matchers) == 0 {
		return i.Match(o, MatchAll(NewMatcher(IdentityMatchesPath), NewMatcher(IdentityEqual)))
	}

	ci, co := maps.Clone(i), maps.Clone(o)
	for _, matcher := range matchers {
		if matcher.Match(ci, co) {
			return true
		}
	}

	return false
}

func MatchAll(matchers ...IdentityMatcher) IdentityMatcher {
	return &andMatcher{matchers: matchers}
}

type andMatcher struct {
	matchers []IdentityMatcher
}

func (a *andMatcher) Match(i, o Identity) bool {
	for _, matcher := range a.matchers {
		if !matcher.Match(i, o) {
			return false
		}
	}
	return true
}
