package versioning_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime/versioning"
)

func calverFull() versioning.Scheme {
	return versioning.NewRegexScheme("calver-full",
		regexp.MustCompile(`^(?P<year>\d{4})\.(?P<month>\d{2})\.(?P<day>\d{2})$`),
		[]string{"year", "month", "day"})
}

func calverUbuntu() versioning.Scheme {
	return versioning.NewRegexScheme("calver-ubuntu",
		regexp.MustCompile(`^(?P<year>\d{2})\.(?P<month>\d{2})$`),
		[]string{"year", "month"})
}

func buildNumber() versioning.Scheme {
	return versioning.NewRegexScheme("build-number",
		regexp.MustCompile(`^(?P<build>\d+)$`),
		[]string{"build"})
}

func TestVersioning_DefaultSemverOrdering(t *testing.T) {
	r := require.New(t)
	reg := versioning.Default()
	versions := []string{"1.0.0", "1.2.0", "1.10.0", "v2.0.0"}
	r.NoError(reg.SortDescending(versions))
	r.Equal([]string{"v2.0.0", "1.10.0", "1.2.0", "1.0.0"}, versions)
}

func TestVersioning_CalverOrdering(t *testing.T) {
	r := require.New(t)
	// calver scheme first, semver default appended as fallback.
	reg := versioning.NewRegistry(calverFull(), versioning.Default().Schemes()[0])
	versions := []string{"2024.03.15", "2024.10.01", "2023.12.31"}
	r.NoError(reg.SortDescending(versions))
	r.Equal([]string{"2024.10.01", "2024.03.15", "2023.12.31"}, versions)
}

func TestVersioning_BuiltinCatalogOrdering(t *testing.T) {
	r := require.New(t)

	ubuntu := versioning.NewRegistry(calverUbuntu())
	uv := []string{"22.04", "22.10", "23.04"}
	r.NoError(ubuntu.SortDescending(uv))
	r.Equal([]string{"23.04", "22.10", "22.04"}, uv, "ubuntu YY.MM must compare month numerically")

	builds := versioning.NewRegistry(buildNumber())
	bv := []string{"1837", "1838", "1900"}
	r.NoError(builds.SortDescending(bv))
	r.Equal([]string{"1900", "1838", "1837"}, bv, "build numbers must compare as integers, not lexically")
}

func TestVersioning_CompareNumericNotLexical(t *testing.T) {
	r := require.New(t)
	builds := versioning.NewRegistry(buildNumber())
	// lexically "1900" < "1838"; numerically 1900 > 1838.
	c, err := builds.Compare("1900", "1838")
	r.NoError(err)
	r.Positive(c)
}

func TestVersioning_FilterSemverConstraint(t *testing.T) {
	r := require.New(t)
	reg := versioning.Default()
	out, err := reg.Filter([]string{"1.0.0", "1.5.0", "2.0.0"}, ">=1.5.0 <2.0.0")
	r.NoError(err)
	r.Equal([]string{"1.5.0"}, out)
}

func TestVersioning_FilterRetainsNonSemverVersions(t *testing.T) {
	r := require.New(t)
	reg := versioning.NewRegistry(calverFull(), versioning.Default().Schemes()[0])
	// A semver constraint does not describe calver versions; they are retained,
	// not dropped, so listing a calver history still works.
	out, err := reg.Filter([]string{"2024.03.15", "2024.10.01"}, "> 0.0.0-0")
	r.NoError(err)
	r.Equal([]string{"2024.03.15", "2024.10.01"}, out)
}

func TestVersioning_FilterMixedSchemes(t *testing.T) {
	r := require.New(t)
	reg := versioning.NewRegistry(calverFull(), versioning.Default().Schemes()[0])
	// semver versions are constrained; calver versions pass through.
	out, err := reg.Filter([]string{"1.0.0", "2.0.0", "2024.03.15"}, "< 2.0.0")
	r.NoError(err)
	r.Equal([]string{"1.0.0", "2024.03.15"}, out)
}

func TestVersioning_FilterEmptyConstraintPassthrough(t *testing.T) {
	r := require.New(t)
	reg := versioning.NewRegistry(calverFull())
	in := []string{"2024.03.15", "2024.10.01"}
	out, err := reg.Filter(in, "")
	r.NoError(err)
	r.Equal(in, out)
}

func TestVersioning_Satisfies(t *testing.T) {
	r := require.New(t)
	reg := versioning.NewRegistry(calverFull(), versioning.Default().Schemes()[0])

	ok, err := reg.Satisfies("1.5.0", ">=1.0.0 <2.0.0")
	r.NoError(err)
	r.True(ok)

	ok, err = reg.Satisfies("2.0.0", ">=1.0.0 <2.0.0")
	r.NoError(err)
	r.False(ok)

	// non-semver version never satisfies a semver constraint (strict gating).
	ok, err = reg.Satisfies("latest", ">=1.0.0")
	r.NoError(err)
	r.False(ok)

	// empty constraint is always satisfied.
	ok, err = reg.Satisfies("anything", "")
	r.NoError(err)
	r.True(ok)

	// malformed constraint errors.
	_, err = reg.Satisfies("1.0.0", "not-a-constraint")
	r.Error(err)
}

func TestVersioning_ValidAndMatch(t *testing.T) {
	r := require.New(t)
	reg := versioning.NewRegistry(calverFull(), versioning.Default().Schemes()[0])
	r.True(reg.Valid("2024.03.15"))
	r.True(reg.Valid("1.2.3"))
	r.False(reg.Valid("not a version"))
}

func TestVersioning_UnknownSchemeFallsBackToLexical(t *testing.T) {
	r := require.New(t)
	reg := versioning.Default() // only semver
	// neither is semver, so lexical fallback, no error.
	c, err := reg.Compare("zeta", "alpha")
	r.NoError(err)
	r.Positive(c)
}

func TestVersioning_MixedSchemeOrderingIsDeterministic(t *testing.T) {
	r := require.New(t)
	// semver scheme with an unknown-scheme value ("1z") mixed in. A pair-dependent
	// fallback would be non-transitive and yield different first elements per
	// permutation; the rank-based order must be stable.
	reg := versioning.Default()
	perms := [][]string{
		{"2.0.0", "10.0.0", "1z"},
		{"2.0.0", "1z", "10.0.0"},
		{"10.0.0", "2.0.0", "1z"},
		{"10.0.0", "1z", "2.0.0"},
		{"1z", "2.0.0", "10.0.0"},
		{"1z", "10.0.0", "2.0.0"},
	}
	var first string
	for i, p := range perms {
		versions := append([]string(nil), p...)
		r.NoError(reg.SortDescending(versions))
		if i == 0 {
			first = versions[0]
			// semver versions rank above the unknown-scheme value, newest first.
			r.Equal([]string{"10.0.0", "2.0.0", "1z"}, versions)
			continue
		}
		r.Equal(first, versions[0], "first element must not depend on input order")
		r.Equal([]string{"10.0.0", "2.0.0", "1z"}, versions)
	}
}

func TestVersioning_BuildNumberBeyondInt64(t *testing.T) {
	r := require.New(t)
	builds := versioning.NewRegistry(buildNumber())
	// 21-digit vs 20-digit build numbers exceed int64; a strconv.Atoi fallback to
	// lexical would sort the shorter (but numerically smaller) value first here.
	small := "99999999999999999999" // 20 nines
	big := "100000000000000000000"  // 21 digits, larger
	c, err := builds.Compare(big, small)
	r.NoError(err)
	r.Positive(c, "arbitrary-precision numeric comparison must not overflow")

	versions := []string{small, big}
	r.NoError(builds.SortDescending(versions))
	r.Equal([]string{big, small}, versions)
}

func TestVersioning_SatisfiesRejectsNonSemverScheme(t *testing.T) {
	r := require.New(t)
	// calver takes priority; "2024.03.15" also parses as semver but its
	// authoritative scheme is calver, so it must not satisfy a semver constraint.
	reg := versioning.NewRegistry(calverFull(), versioning.Default().Schemes()[0])
	ok, err := reg.Satisfies("2024.03.15", ">=1.0.0")
	r.NoError(err)
	r.False(ok)
	// a genuine semver still evaluates against the constraint.
	ok, err = reg.Satisfies("2.0.0", ">=1.0.0")
	r.NoError(err)
	r.True(ok)
}

func TestVersioning_ExplicitLooseSemverSchemeOrdersAndRanks(t *testing.T) {
	r := require.New(t)
	// calver first, an explicit built-in loose-semver scheme second (fallback).
	reg := versioning.NewRegistry(calverFull(), versioning.NewLooseSemverScheme())
	r.True(reg.Valid("2024.03.15"))
	r.True(reg.Valid("1.2.3"))

	// calver (rank 0) sorts ahead of semver (rank 1); each group ordered by its
	// own scheme.
	versions := []string{"1.2.3", "2024.03.15", "1.10.0", "2023.12.31"}
	r.NoError(reg.SortDescending(versions))
	r.Equal([]string{"2024.03.15", "2023.12.31", "1.10.0", "1.2.3"}, versions)
}

func TestVersioning_RegexSchemeSatisfiesRelational(t *testing.T) {
	r := require.New(t)
	calver := calverFull()

	cases := []struct {
		constraint string
		want       bool
	}{
		{">=2024.01.01", true},
		{">=2024.06.01", false},
		{">=2024.03.15 <2024.10.01", true},
		{"<2024.03.15", false},
		{"<=2024.03.15", true},
		{"2024.03.15", true}, // bare operand means equality
		{"!=2024.03.15", false},
		{">=2024.06.01, <2025.01.01", false}, // comma-composed AND; first term fails
		{">= 2024.03.15", true},              // whitespace between operator and operand
		{">= 2024.06.01", false},
		{">=  2024.03.15", true},                 // multiple spaces after operator
		{">= 2024.03.15 < 2024.10.01", true},     // spaced operators in a whitespace conjunction
		{">= 2024.06.01 , <= 2024.10.01", false}, // spaced operators around a comma
		{"", true},
	}
	for _, tc := range cases {
		ok, err := calver.Satisfies("2024.03.15", tc.constraint)
		r.NoError(err, "constraint %q", tc.constraint)
		r.Equal(tc.want, ok, "constraint %q", tc.constraint)
	}

	// An operand that is not a version of this scheme means the constraint belongs
	// to a foreign grammar: not applicable, not a hard error.
	_, err := calver.Satisfies("2024.03.15", ">=not-a-date")
	r.ErrorIs(err, versioning.ErrConstraintNotApplicable)
	r.ErrorIs(calver.ValidateConstraint(">=not-a-date"), versioning.ErrConstraintNotApplicable)
	r.NoError(calver.ValidateConstraint(">=2024.01.01 <2025.01.01"))
}

func TestVersioning_FilterForeignAndMalformedConstraints(t *testing.T) {
	r := require.New(t)

	// Foreign grammar: a semver range over a calver-only registry is not
	// applicable, so calver versions are retained (used by "get cv" whose default
	// constraint "> 0.0.0-0" is semver).
	regCal := versioning.NewRegistry(calverFull())
	out, err := regCal.Filter([]string{"2024.03.15", "2024.10.01"}, "> 0.0.0-0")
	r.NoError(err)
	r.Equal([]string{"2024.03.15", "2024.10.01"}, out)

	// Foreign grammar in a mixed registry: calver retained, semver constrained.
	regMix := versioning.NewRegistry(calverFull(), versioning.NewLooseSemverScheme())
	out, err = regMix.Filter([]string{"2024.03.15", "1.0.0", "2.0.0"}, ">=2.0.0")
	r.NoError(err)
	r.Equal([]string{"2024.03.15", "2.0.0"}, out)

	// Malformed semver: a semver version with an unparseable constraint errors.
	_, err = versioning.Default().Filter([]string{"1.0.0"}, "not-a-constraint")
	r.Error(err)
}

func TestVersioning_RegexSchemeSatisfiesNumeric(t *testing.T) {
	r := require.New(t)
	builds := buildNumber()
	// Numeric ordering, not lexical: 12000 >= 1000 holds though "12000" < "1000".
	ok, err := builds.Satisfies("12000", ">=1000")
	r.NoError(err)
	r.True(ok)
	ok, err = builds.Satisfies("1837", ">=2000")
	r.NoError(err)
	r.False(ok)
}

func TestVersioning_FilterCalverRelationalConstraint(t *testing.T) {
	r := require.New(t)
	reg := versioning.NewRegistry(calverFull())

	out, err := reg.Filter([]string{"2024.03.15", "2024.10.01", "2023.12.31"}, ">=2024.01.01")
	r.NoError(err)
	r.Equal([]string{"2024.03.15", "2024.10.01"}, out) // input order preserved

	out, err = reg.Filter([]string{"2024.03.15", "2024.10.01"}, ">=2024.06.01 <2025.01.01")
	r.NoError(err)
	r.Equal([]string{"2024.10.01"}, out)
}

func TestVersioning_SatisfiesCalverRelationalAndUnknown(t *testing.T) {
	r := require.New(t)
	reg := versioning.NewRegistry(calverFull())

	ok, err := reg.Satisfies("2024.10.01", ">=2024.06.01")
	r.NoError(err)
	r.True(ok)
	ok, err = reg.Satisfies("2024.03.15", ">=2024.06.01")
	r.NoError(err)
	r.False(ok)

	// A version no scheme claims never satisfies a constraint.
	ok, err = reg.Satisfies("zzz", ">=2024.01.01")
	r.NoError(err)
	r.False(ok)
}

// TestVersioning_CompareMixedNumericTextIsTransitive guards against the
// non-transitive pair-dependent numeric/lexical switch: with a single
// alphanumeric capture group, "2", "10" and "1a" must sort deterministically
// regardless of input permutation (numeric values before non-numeric ones).
func TestVersioning_CompareMixedNumericTextIsTransitive(t *testing.T) {
	r := require.New(t)
	scheme := versioning.NewRegexScheme("alnum",
		regexp.MustCompile(`^(?P<value>[0-9a-z]+)$`),
		[]string{"value"})
	reg := versioning.NewRegistry(scheme)

	// numeric a < numeric b, numeric < non-numeric, and the order is total.
	c, err := reg.Compare("10", "2")
	r.NoError(err)
	r.Positive(c) // 10 > 2 numerically
	c, err = reg.Compare("1a", "10")
	r.NoError(err)
	r.Positive(c) // non-numeric "1a" sorts after numeric "10"
	c, err = reg.Compare("1a", "2")
	r.NoError(err)
	r.Positive(c) // transitive: since 1a > 10 > 2, 1a > 2 as well

	perms := [][]string{
		{"2", "10", "1a"},
		{"1a", "2", "10"},
		{"10", "1a", "2"},
		{"1a", "10", "2"},
		{"2", "1a", "10"},
		{"10", "2", "1a"},
	}
	for _, p := range perms {
		versions := append([]string(nil), p...)
		r.NoError(reg.SortDescending(versions))
		// descending: non-numeric "1a" newest, then 10, then 2.
		r.Equal([]string{"1a", "10", "2"}, versions, "input %v", p)
	}
}

// TestVersioning_ConstraintOperatorWithoutOperandRejected guards against a
// relational operator with no operand being silently dropped (which made ">="
// match everything).
func TestVersioning_ConstraintOperatorWithoutOperandRejected(t *testing.T) {
	r := require.New(t)
	calver := calverFull()

	for _, c := range []string{">=", "<", ">=2024.03.15 <", ">= "} {
		_, err := calver.Satisfies("2024.03.15", c)
		r.Error(err, "constraint %q must be rejected", c)
		r.Error(calver.ValidateConstraint(c), "constraint %q must be rejected", c)
	}

	reg := versioning.NewRegistry(calver)
	_, err := reg.Filter([]string{"2024.03.15", "2024.10.01"}, ">=")
	r.Error(err)
	_, err = reg.Satisfies("2024.03.15", ">=")
	r.Error(err)
}

// TestVersioning_ForeignConstraintHandlingIsSymmetric guards the documented
// lenient policy in both directions: a foreign but well-formed relational
// constraint retains the histories its scheme cannot interpret, rather than
// aborting once a semver fallback is present.
func TestVersioning_ForeignConstraintHandlingIsSymmetric(t *testing.T) {
	r := require.New(t)
	build := versioning.NewRegexScheme("build",
		regexp.MustCompile(`^build-(?P<n>\d+)$`),
		[]string{"n"})

	// build-only history with a build-N constraint filters correctly.
	regBuild := versioning.NewRegistry(build)
	out, err := regBuild.Filter([]string{"build-9", "build-100"}, ">=build-100")
	r.NoError(err)
	r.Equal([]string{"build-100"}, out)

	// Adding a semver fallback must not turn the same query into an error: the
	// semver versions cannot interpret the build constraint, so they are retained
	// (foreign history), while the build versions are filtered.
	regMix := versioning.NewRegistry(build, versioning.NewLooseSemverScheme())
	out, err = regMix.Filter([]string{"build-9", "build-100", "1.0.0", "2.0.0"}, ">=build-100")
	r.NoError(err)
	r.Equal([]string{"build-100", "1.0.0", "2.0.0"}, out)

	// The reverse direction already worked and must keep working: a semver
	// constraint retains the build history and filters the semver versions.
	out, err = regMix.Filter([]string{"build-9", "1.0.0", "2.0.0"}, ">=2.0.0")
	r.NoError(err)
	r.Equal([]string{"build-9", "2.0.0"}, out)

	// A constraint no scheme can parse is still malformed and errors.
	_, err = regMix.Filter([]string{"1.0.0"}, "not-a-constraint")
	r.Error(err)
}
