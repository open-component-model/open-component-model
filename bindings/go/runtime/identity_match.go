package runtime

import (
	"maps"
	"path"
)

// IdentityMatchingFn is a function that takes two identities and returns if they match.
// It is expected that the function can modify the identities in place.
// If a comparison is absolute, the function can choose to delete attributes.
// This has an effect of following IdentityMatcher's if used in a chain with Identity.Match.
type IdentityMatchingFn func(Identity, Identity) bool

type IdentityMatcher interface {
	Match(Identity, Identity) bool
}

// functionalIdentityMatcher is a wrapper around IdentityMatchingFn that implements IdentityMatcher.
type functionalIdentityMatcher struct {
	fn IdentityMatchingFn
}

// Match delegates to the IdentityMatchingFn.
func (f *functionalIdentityMatcher) Match(a, b Identity) bool {
	return f.fn(a, b)
}

// NewMatcher creates a new IdentityMatcher from a IdentityMatchingFn.
func NewMatcher(fn IdentityMatchingFn) IdentityMatcher {
	return &functionalIdentityMatcher{fn: fn}
}

// IdentityIsContained is a matcher that checks if the identity i is contained in the identity o.
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

// IdentityEqual is an equality IdentityMatchingFn. see Identity.Equal for more information
func IdentityEqual(a Identity, b Identity) bool {
	del := func(s string, s2 string) bool {
		return true
	}
	defer func() {
		maps.DeleteFunc(a, del)
		maps.DeleteFunc(b, del)
	}()
	return a.Equal(b)
}

// Match returns true if the identity a matches the identity b.
// It uses the provided Matchers to determine the match.
// If no Matchers are provided, it uses IdentityMatchesPath and IdentityEqual in order.
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

// MatchAll is a convenience function that creates an AndMatcher that matches all provided matchers.
// In other words, it returns true if all given IdentityMatcher.Match return true.
func MatchAll(matchers ...IdentityMatcher) IdentityMatcher {
	return &AndMatcher{Matchers: matchers}
}

// AndMatcher is a matcher that matches if all provided matchers match.
type AndMatcher struct {
	Matchers []IdentityMatcher
}

// Match returns true if all matchers match.
func (a *AndMatcher) Match(i, o Identity) bool {
	for _, matcher := range a.Matchers {
		if !matcher.Match(i, o) {
			return false
		}
	}
	return true
}
