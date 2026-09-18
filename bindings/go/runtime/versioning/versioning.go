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
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// ErrConstraintNotApplicable signals that a constraint cannot be expressed in a
// scheme's grammar — for example a semver range applied to a calver scheme whose
// operands are dates, not semver versions. It is distinct from a malformed
// constraint (a syntax error in the scheme's own grammar). A registry retains a
// not-applicable version when listing ([Registry.Filter]) and treats it as not
// matching when gating ([Registry.Satisfies]), so a constraint never silently
// discards a history expressed in a different scheme.
var ErrConstraintNotApplicable = errors.New("constraint not applicable to scheme")

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
	// Satisfies reports whether a version this scheme claims satisfies the given
	// constraint, interpreted in the scheme's own grammar. An empty constraint is
	// satisfied by any claimed version. It returns an error if the version is not
	// claimed or the constraint is not well-formed for this scheme (see
	// ValidateConstraint).
	Satisfies(version, constraint string) (bool, error)
	// ValidateConstraint reports whether the constraint expression is well-formed
	// in this scheme's grammar, independent of any version. An empty constraint
	// is always valid. It returns an error describing the first problem otherwise.
	ValidateConstraint(constraint string) error
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
	return NewRegistry(NewLooseSemverScheme())
}

// NewLooseSemverScheme returns the built-in loose-semver [Scheme], wrapping
// github.com/Masterminds/semver/v3. Configuration packages reference it to opt
// the built-in semver behavior back in explicitly (e.g. as a trailing fallback)
// once custom schemes are configured.
func NewLooseSemverScheme() Scheme {
	return newLooseSemverScheme()
}

// Schemes returns the registry's schemes in priority order.
func (r *Registry) Schemes() []Scheme {
	return r.schemes
}

// Compare orders two versions with a total, transitive order that is stable
// across input permutations.
//
// Each version is resolved to the rank of the first scheme that claims it (its
// index in the registry), or an explicit "unknown" rank beyond all schemes when
// no scheme claims it. Versions resolving to different ranks are ordered by rank
// (higher-priority schemes first). Versions sharing a scheme rank are ordered by
// that scheme's Compare; two unknown-rank versions compare lexically. This
// avoids the non-transitive cycles a pair-dependent fallback would create when
// mixing schemes.
func (r *Registry) Compare(a, b string) (int, error) {
	ra, sa := r.rankFor(a)
	rb, _ := r.rankFor(b)
	if ra != rb {
		// A lower rank index means a higher-priority scheme; treat those versions
		// as "greater" so they sort ahead under descending order.
		return cmp.Compare(rb, ra), nil
	}
	if sa == nil {
		// Both versions share the unknown rank; order lexically.
		return strings.Compare(a, b), nil
	}
	return sa.Compare(a, b)
}

// Valid reports whether any registered scheme considers the version well-formed.
func (r *Registry) Valid(version string) bool {
	for _, s := range r.schemes {
		if s.Matches(version) && s.Valid(version) {
			return true
		}
	}
	return false
}

// SortDescending sorts versions in place, newest first, using [Registry.Compare].
// The sort is stable so equal versions retain their input order. It returns the
// first error a scheme's Compare reports; on error the slice may be left
// partially reordered.
func (r *Registry) SortDescending(versions []string) error {
	var cmpErr error
	sort.SliceStable(versions, func(i, j int) bool {
		if cmpErr != nil {
			return false
		}
		c, err := r.Compare(versions[i], versions[j])
		if err != nil {
			cmpErr = err
			return false
		}
		return c > 0
	})
	return cmpErr
}

// Filter returns the versions satisfying the given constraint.
//
// The constraint is interpreted by each version's authoritative scheme, not
// globally as semver: loose-semver versions use semver range syntax (">=1.0.0",
// "^1.2"), while a regex scheme uses its own relational grammar (">=2024.03.15
// <2024.10.01"). A version no scheme claims is retained unchanged, so a
// constraint never silently discards an unrecognized history.
//
// An empty constraint returns the input unchanged. A constraint no registered
// scheme can parse is malformed and returns an error.
func (r *Registry) Filter(versions []string, constraint string) ([]string, error) {
	if constraint == "" {
		return versions, nil
	}
	filtered := make([]string, 0, len(versions))
	for _, version := range versions {
		// A version no scheme claims, or one whose scheme cannot express the
		// constraint (a foreign grammar, e.g. a semver range over a calver
		// history), is retained unchanged so a constraint never silently discards
		// an unrecognized history. A constraint malformed in the scheme's own
		// grammar surfaces as an error.
		s := r.schemeFor(version)
		if s == nil {
			filtered = append(filtered, version)
			continue
		}
		ok, err := s.Satisfies(version, constraint)
		if errors.Is(err, ErrConstraintNotApplicable) {
			filtered = append(filtered, version)
			continue
		}
		if err != nil {
			return nil, err
		}
		if ok {
			filtered = append(filtered, version)
		}
	}
	return filtered, nil
}

// Satisfies reports whether a single version satisfies the given constraint,
// interpreted by the version's authoritative scheme (semver range syntax for
// loose-semver versions, a relational grammar for regex schemes). Unlike
// [Registry.Filter], this is a strict membership test intended for gating (e.g.
// resolver version constraints): a version no scheme claims is treated as not
// matching.
//
// An empty constraint is satisfied by any version. A constraint no registered
// scheme can parse is malformed and returns an error.
func (r *Registry) Satisfies(version, constraint string) (bool, error) {
	if constraint == "" {
		return true, nil
	}
	// A version no scheme claims, or one whose scheme cannot express the
	// constraint (a foreign grammar), never satisfies it (strict gate). A
	// constraint malformed in the scheme's own grammar surfaces as an error.
	s := r.schemeFor(version)
	if s == nil {
		return false, nil
	}
	ok, err := s.Satisfies(version, constraint)
	if errors.Is(err, ErrConstraintNotApplicable) {
		return false, nil
	}
	return ok, err
}

// ValidateConstraint reports whether the constraint is well-formed for at least
// one registered scheme, independent of any version. An empty constraint is
// always valid. This is intended for eager validation at configuration load
// time, so an unusable constraint fails fast rather than silently matching
// nothing later. It returns the loose-semver parse error when a semver scheme is
// present, otherwise a joined error across all schemes.
func (r *Registry) ValidateConstraint(constraint string) error {
	if constraint == "" {
		return nil
	}
	var malformed []error
	for _, s := range r.schemes {
		err := s.ValidateConstraint(constraint)
		if err == nil {
			return nil // a scheme accepts the constraint in its own grammar.
		}
		if !errors.Is(err, ErrConstraintNotApplicable) {
			// A genuine syntax error (e.g. a malformed semver range) rather than a
			// constraint that merely belongs to a different scheme's grammar.
			malformed = append(malformed, err)
		}
	}
	// Every scheme rejected the constraint. If any rejection was a real syntax
	// error, the constraint is malformed; otherwise it is simply not applicable to
	// any configured scheme (still a usable configuration — it just matches
	// nothing until a suitable scheme is added), which is not an error.
	return errors.Join(malformed...)
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

// rankFor resolves a version to the index of the first scheme that claims it
// and that scheme. When no scheme claims the version it returns the
// unknown-scheme rank (len(schemes)) and a nil scheme. The rank gives a stable,
// transitive ordering across mixed schemes; see [Registry.Compare].
func (r *Registry) rankFor(version string) (int, Scheme) {
	for i, s := range r.schemes {
		if s.Matches(version) {
			return i, s
		}
	}
	return len(r.schemes), nil
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

// Satisfies evaluates a semver range constraint (e.g. ">=1.0.0 <2.0.0", "^1.2")
// against a semver version. A version that does not parse as semver never
// satisfies a constraint. A malformed constraint returns an error.
func (looseSemverScheme) Satisfies(version, constraint string) (bool, error) {
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

// ValidateConstraint reports whether the constraint is a well-formed semver
// range.
func (looseSemverScheme) ValidateConstraint(constraint string) error {
	if constraint == "" {
		return nil
	}
	if _, err := semver.NewConstraint(constraint); err != nil {
		return fmt.Errorf("parsing semantic version constraint failed: %w", err)
	}
	return nil
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

// Satisfies evaluates a relational constraint against a version this scheme
// claims. The constraint is a whitespace- or comma-separated conjunction of
// terms; each term is a relational operator (">=", ">", "<=", "<", "=", "==",
// "!=") followed by a version operand that must itself match the scheme's
// pattern. A bare operand with no operator means equality. Ordering follows the
// scheme's own [regexScheme.Compare], so it is identical to the sort order.
//
// Unlike loose semver, range operators such as "^", "~", and x-ranges are not
// supported: they have no scheme-independent meaning.
func (s *regexScheme) Satisfies(version, constraint string) (bool, error) {
	if constraint == "" {
		return true, nil
	}
	if !s.pattern.MatchString(version) {
		return false, fmt.Errorf("version %q does not match scheme %q", version, s.name)
	}
	if err := s.ValidateConstraint(constraint); err != nil {
		return false, err
	}
	for _, term := range splitConstraintTerms(constraint) {
		op, operand := parseConstraintTerm(term)
		c, err := s.Compare(version, operand)
		if err != nil {
			return false, err
		}
		if !satisfiesOp(op, c) {
			return false, nil
		}
	}
	return true, nil
}

// ValidateConstraint reports whether every term of the relational constraint is
// parseable and its operand matches the scheme pattern.
func (s *regexScheme) ValidateConstraint(constraint string) error {
	if constraint == "" {
		return nil
	}
	for _, term := range splitConstraintTerms(constraint) {
		_, operand := parseConstraintTerm(term)
		if !s.pattern.MatchString(operand) {
			// A regex scheme has no notion of a syntactically malformed constraint:
			// any operand that is not one of its versions simply means the
			// constraint belongs to a different grammar.
			return fmt.Errorf("scheme %q: constraint operand %q does not match the scheme pattern: %w", s.name, operand, ErrConstraintNotApplicable)
		}
	}
	return nil
}

// constraintOperators lists the relational operators a regex scheme constraint
// term may start with, longest first so the two-character operators win over
// their single-character prefixes.
var constraintOperators = []string{">=", "<=", "!=", "==", "=", ">", "<"}

// splitConstraintTerms splits a constraint expression into conjunctive terms on
// commas and runs of whitespace, dropping empty terms.
func splitConstraintTerms(constraint string) []string {
	return strings.FieldsFunc(constraint, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
}

// parseConstraintTerm strips a leading relational operator from a term and
// returns the operator and the trimmed version operand. A term with no
// recognized operator defaults to equality ("=").
func parseConstraintTerm(term string) (op, operand string) {
	for _, candidate := range constraintOperators {
		if strings.HasPrefix(term, candidate) {
			return candidate, strings.TrimSpace(strings.TrimPrefix(term, candidate))
		}
	}
	return "=", strings.TrimSpace(term)
}

// satisfiesOp reports whether a [regexScheme.Compare] result c (negative when
// version < operand) satisfies the relational operator op.
func satisfiesOp(op string, c int) bool {
	switch op {
	case ">=":
		return c >= 0
	case ">":
		return c > 0
	case "<=":
		return c <= 0
	case "<":
		return c < 0
	case "!=":
		return c != 0
	default: // "=", "=="
		return c == 0
	}
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

// compareGroup compares two capture-group values, numerically when both are
// decimal integers and lexically otherwise. Numeric comparison uses arbitrary
// precision so arbitrarily large groups (e.g. long build numbers) never overflow
// a machine integer and fall back to lexical ordering.
func compareGroup(a, b string) int {
	na, aok := new(big.Int).SetString(a, 10)
	nb, bok := new(big.Int).SetString(b, 10)
	if aok && bok {
		return na.Cmp(nb)
	}
	return strings.Compare(a, b)
}
