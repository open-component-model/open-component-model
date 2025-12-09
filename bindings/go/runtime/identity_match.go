package runtime

import (
	"maps"
	"path"
)

// IdentityMatchingChainFn is a function that takes two identities and returns if they match.
// It is expected that the function can modify the identities in place.
// If a comparison is absolute, the function can choose to delete attributes.
// This has an effect of following ChainableIdentityMatcher's if used in a chain with Identity.Match.
type IdentityMatchingChainFn func(Identity, Identity) bool

type ChainableIdentityMatcher interface {
	Match(Identity, Identity) bool
}

// Match delegates to the IdentityMatchingChainFn.
func (f IdentityMatchingChainFn) Match(a, b Identity) bool {
	return f(a, b)
}

// IdentityMatchesPath returns true if the identity a matches the subpath of the identity b.
// If the path attribute is not set in either identity, it returns true.
// If the path attribute is set in both identities,
// it returns true if the path attribute of b contains the path attribute of a.
// For more information, check path.Match.
// IdentityMatchesPath deletes the path attribute from both identities, because it is expected
// that it is used in a chain with Identity.Match and the authority decision of the path attribute.
//
// see IdentityMatchingChainFn and Identity.Match for more information.
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

// IdentityMatchesURL returns true if the URL components of two identities match,
// applying default port mapping based on scheme.
// - HTTPS defaults to port 443 if not explicitly set
// - HTTP defaults to port 80 if not explicitly set
// Allows identities differing only in explicit vs implicit ports to match.
// Deletes the URL attributes (scheme, hostname, port) from both identities
// after comparison, as it is expected to be used in a chain with Identity.Match.
//
// see IdentityMatchingChainFn and Identity.Match for more information.
func IdentityMatchesURL(i, o Identity) bool {
	iScheme := i[IdentityAttributeScheme]
	oScheme := o[IdentityAttributeScheme]
	iHost := i[IdentityAttributeHostname]
	oHost := o[IdentityAttributeHostname]
	iPort := i[IdentityAttributePort]
	oPort := o[IdentityAttributePort]

	delete(i, IdentityAttributeScheme)
	delete(i, IdentityAttributeHostname)
	delete(i, IdentityAttributePort)
	delete(o, IdentityAttributeScheme)
	delete(o, IdentityAttributeHostname)
	delete(o, IdentityAttributePort)

	if iScheme != oScheme {
		return false
	}
	if iHost != oHost {
		return false
	}

	if iPort == "" && iScheme != "" {
		switch iScheme {
		case "https":
			iPort = "443"
		case "http":
			iPort = "80"
		}
	}

	if oPort == "" && oScheme != "" {
		switch oScheme {
		case "https":
			oPort = "443"
		case "http":
			oPort = "80"
		}
	}

	return iPort == oPort
}

// IdentityEqual is an equality IdentityMatchingChainFn. see Identity.Equal for more information
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

// IdentitySubset is a ChainableIdentityMatcher that checks if the identity sub is a subset of the identity base.
// It is useful to check if an identity is a subset of another identity, and thus can be considered
// an IdentityEqual matcher on a subset of the identity.
//
// Note that giving an empty subset will always return true, as an empty identity is a subset of any identity.
func IdentitySubset(sub Identity, base Identity) bool {
	if len(sub) > len(base) {
		return false
	}
	for candidateKey, candidateValue := range sub {
		valueInBase, found := base[candidateKey]
		mismatch := !found || valueInBase != candidateValue
		if mismatch {
			return false
		}
	}
	return true
}

// Match returns true if the identity a matches the identity b.
// It uses the provided Matchers to determine the match.
// If no Matchers are provided, it uses IdentityMatchesPath, IdentityMatchesURL, and IdentityEqual in order.
// If any matcher returns false, it returns false.
func (i Identity) Match(o Identity, matchers ...ChainableIdentityMatcher) bool {
	if len(matchers) == 0 {
		return i.Match(o, MatchAll(
			IdentityMatchingChainFn(IdentityMatchesPath),
			IdentityMatchingChainFn(IdentityMatchesURL),
			IdentityMatchingChainFn(IdentityEqual),
		))
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
// In other words, it returns true if all given ChainableIdentityMatcher.Match return true.
func MatchAll(matchers ...ChainableIdentityMatcher) ChainableIdentityMatcher {
	return &AndMatcher{Matchers: matchers}
}

// AndMatcher is a matcher that matches if all provided matchers match.
type AndMatcher struct {
	Matchers []ChainableIdentityMatcher
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
