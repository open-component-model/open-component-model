// Package versioning provides a pluggable, configuration-driven abstraction for
// matching, comparing, sorting, and filtering OCM component version strings.
//
// Historically OCM assumed loose semantic versioning everywhere a version was
// parsed. This package generalizes that: a [Registry] holds an ordered list of
// [Scheme] implementations, and every version-handling site consults the
// registry instead of calling a semver library directly. The default registry
// ([Default]) contains a single loose-semver scheme, so behavior is unchanged
// unless a caller supplies additional schemes (for example calver or monotonic
// build numbers) built from a versioning configuration.
//
// This package is a leaf: it MUST NOT import the runtime type system or the
// configuration packages, so that a configuration type may import it without
// creating an import cycle.
package versioning

import (
	"cmp"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// Scheme describes how a single family of version strings is recognized and
// ordered.
type Scheme interface {
	// Name is a stable identifier for the scheme (e.g. "loose-semver", "calver").
	Name() string
	// Matches reports whether this scheme claims the given version string.
	Matches(version string) bool
	// Compare orders two versions this scheme claims. The result is negative if
	// a < b, zero if a == b, positive if a > b, following the convention of
	// [cmp.Compare]. It returns an error if either version cannot be parsed
	// under this scheme.
	Compare(a, b string) (int, error)
	// Valid reports whether the version is well-formed for this scheme. For
	// regex schemes this is identical to Matches.
	Valid(version string) bool
}

// Registry is an ordered collection of [Scheme] implementations.
//
// The order is significant: for any operation the first scheme that claims the
// relevant version(s) wins. A registry always behaves deterministically even
// for versions no scheme claims (see [Registry.Compare]).
type Registry struct {
	schemes []Scheme
}

// NewRegistry builds a registry from the given schemes in priority order.
func NewRegistry(schemes ...Scheme) *Registry {
	return &Registry{schemes: schemes}
}

// Default returns a registry containing only the loose-semver scheme. This
// reproduces OCM's historical version behavior exactly.
func Default() *Registry {
	return NewRegistry(newLooseSemverScheme())
}

// Schemes returns the registry's schemes in priority order.
func (r *Registry) Schemes() []Scheme {
	return r.schemes
}

// Compare orders two versions. It selects the first scheme that claims both
// versions and delegates to it. If no single scheme claims both, it falls back
// to lexical comparison and never returns an error, guaranteeing a stable,
// deterministic order even for unknown or mixed schemes.
func (r *Registry) Compare(a, b string) (int, error) {
	for _, s := range r.schemes {
		if s.Matches(a) && s.Matches(b) {
			return s.Compare(a, b)
		}
	}
	return strings.Compare(a, b), nil
}

// Valid reports whether any registered scheme claims the version.
func (r *Registry) Valid(version string) bool {
	for _, s := range r.schemes {
		if s.Matches(version) {
			return true
		}
	}
	return false
}

// SortDescending sorts versions in place, newest first, using [Registry.Compare].
// The sort is stable so equal versions retain their input order.
func (r *Registry) SortDescending(versions []string) {
	sort.SliceStable(versions, func(i, j int) bool {
		c, _ := r.Compare(versions[i], versions[j])
		return c > 0
	})
}

// Filter returns the versions satisfying the given constraint.
//
// Version constraints (e.g. ">=1.0.0", "^1.2") are a semver concept. The
// constraint is applied to versions that parse as semver; versions belonging to
// a non-semver scheme (e.g. calver) do not satisfy a semver constraint's notion
// of ordering and are retained unchanged, so a semver constraint never silently
// discards an entire non-semver history. An empty or unparseable-as-semver
// constraint that is nonetheless a valid semver range still applies to semver
// versions only.
//
// An empty constraint returns the input unchanged. A malformed constraint
// returns an error.
func (r *Registry) Filter(versions []string, constraint string) ([]string, error) {
	if constraint == "" {
		return versions, nil
	}
	constraints, err := semver.NewConstraint(constraint)
	if err != nil {
		return nil, fmt.Errorf("parsing semantic version constraint failed: %w", err)
	}
	filtered := make([]string, 0, len(versions))
	for _, version := range versions {
		// Only versions whose resolved scheme is loose semver are subject to a
		// semver constraint. Versions claimed first by a non-semver scheme (e.g.
		// calver) are retained, so a semver constraint never discards a
		// non-semver history — even when the version also happens to parse as
		// semver (e.g. "2024.03.15").
		if _, ok := r.schemeFor(version).(*looseSemverScheme); !ok {
			filtered = append(filtered, version)
			continue
		}
		v, err := semver.NewVersion(version)
		if err != nil {
			filtered = append(filtered, version)
			continue
		}
		if !constraints.Check(v) {
			continue
		}
		filtered = append(filtered, version)
	}
	return filtered, nil
}

// Satisfies reports whether a single version satisfies the given semver
// constraint. Constraints are a semver concept: a version that does not parse
// as semver never satisfies one. Unlike [Registry.Filter], this is a strict
// membership test intended for gating (e.g. resolver version constraints),
// where a non-evaluable version must be treated as not matching.
//
// An empty constraint is satisfied by any version. A malformed constraint
// returns an error.
func (r *Registry) Satisfies(version, constraint string) (bool, error) {
	if constraint == "" {
		return true, nil
	}
	constraints, err := semver.NewConstraint(constraint)
	if err != nil {
		return false, fmt.Errorf("parsing semantic version constraint failed: %w", err)
	}
	v, err := semver.NewVersion(version)
	if err != nil {
		return false, nil
	}
	return constraints.Check(v), nil
}

// schemeFor returns the first scheme that claims the version, or nil if none do.
func (r *Registry) schemeFor(version string) Scheme {
	for _, s := range r.schemes {
		if s.Matches(version) {
			return s
		}
	}
	return nil
}

// looseSemverScheme is the default scheme, wrapping github.com/Masterminds/semver/v3.
type looseSemverScheme struct{}

func newLooseSemverScheme() *looseSemverScheme { return &looseSemverScheme{} }

func (looseSemverScheme) Name() string { return "loose-semver" }

func (looseSemverScheme) Matches(version string) bool {
	_, err := semver.NewVersion(version)
	return err == nil
}

func (s looseSemverScheme) Valid(version string) bool { return s.Matches(version) }

func (looseSemverScheme) Compare(a, b string) (int, error) {
	va, err := semver.NewVersion(a)
	if err != nil {
		return 0, fmt.Errorf("parsing version %q failed: %w", a, err)
	}
	vb, err := semver.NewVersion(b)
	if err != nil {
		return 0, fmt.Errorf("parsing version %q failed: %w", b, err)
	}
	return va.Compare(vb), nil
}

// regexScheme claims versions matching a regular expression and orders them by
// a list of named capture groups, most significant first.
type regexScheme struct {
	name             string
	pattern          *regexp.Regexp
	comparisonGroups []string
}

// NewRegexScheme builds a scheme that claims versions matching pattern and
// orders them by the named capture groups in comparisonGroups (most significant
// first). Numeric groups are compared as integers; non-numeric groups compare
// lexically. When comparisonGroups is empty, whole matched strings compare
// lexically.
func NewRegexScheme(name string, pattern *regexp.Regexp, comparisonGroups []string) Scheme {
	return &regexScheme{name: name, pattern: pattern, comparisonGroups: comparisonGroups}
}

func (s *regexScheme) Name() string { return s.name }

func (s *regexScheme) Matches(version string) bool { return s.pattern.MatchString(version) }

func (s *regexScheme) Valid(version string) bool { return s.Matches(version) }

func (s *regexScheme) Compare(a, b string) (int, error) {
	if !s.pattern.MatchString(a) {
		return 0, fmt.Errorf("version %q does not match scheme %q", a, s.name)
	}
	if !s.pattern.MatchString(b) {
		return 0, fmt.Errorf("version %q does not match scheme %q", b, s.name)
	}
	if len(s.comparisonGroups) == 0 {
		return strings.Compare(a, b), nil
	}
	ga := s.groups(a)
	gb := s.groups(b)
	for _, name := range s.comparisonGroups {
		if c := compareGroup(ga[name], gb[name]); c != 0 {
			return c, nil
		}
	}
	return 0, nil
}

// groups extracts the named capture groups of a matched version into a map.
func (s *regexScheme) groups(version string) map[string]string {
	match := s.pattern.FindStringSubmatch(version)
	out := make(map[string]string, len(s.pattern.SubexpNames()))
	for i, name := range s.pattern.SubexpNames() {
		if name == "" || i >= len(match) {
			continue
		}
		out[name] = match[i]
	}
	return out
}

// compareGroup compares two capture-group values, numerically when both parse
// as integers and lexically otherwise.
func compareGroup(a, b string) int {
	na, ea := strconv.Atoi(a)
	nb, eb := strconv.Atoi(b)
	if ea == nil && eb == nil {
		return cmp.Compare(na, nb)
	}
	return strings.Compare(a, b)
}
