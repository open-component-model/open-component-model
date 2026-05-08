package runtime

// TypedMatch returns true if the two Typed values match.
//
// It is the polymorphic counterpart to Identity.Match: both sides are first
// projected to the canonical Identity view via TypedToIdentity, then matched
// using the existing chainable matchers. That projection makes TypedMatch
// uniformly applicable to native typed structs, in-process plugin types, and
// Raw values arriving from out-of-process or non-Go plugins.
//
// The matcher semantics are identical to Identity.Match:
//   - With no matchers, the default chain is used (path glob, URL with
//     default-port semantics, then equality of remaining attributes).
//   - With one or more matchers, they are combined with OR semantics; wrap
//     them in MatchAll for AND semantics.
//
// A projection failure on either side returns false rather than panicking.
// This preserves graph-walking liveness when an opaque plugin value cannot
// be projected — such values simply do not match.
func TypedMatch(a, b Typed, matchers ...ChainableIdentityMatcher) bool {
	ia, err := TypedToIdentity(a)
	if err != nil {
		return false
	}
	ib, err := TypedToIdentity(b)
	if err != nil {
		return false
	}
	return ia.Match(ib, matchers...)
}
