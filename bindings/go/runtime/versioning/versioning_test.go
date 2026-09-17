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
	reg.SortDescending(versions)
	r.Equal([]string{"v2.0.0", "1.10.0", "1.2.0", "1.0.0"}, versions)
}

func TestVersioning_CalverOrdering(t *testing.T) {
	r := require.New(t)
	// calver scheme first, semver default appended as fallback.
	reg := versioning.NewRegistry(calverFull(), versioning.Default().Schemes()[0])
	versions := []string{"2024.03.15", "2024.10.01", "2023.12.31"}
	reg.SortDescending(versions)
	r.Equal([]string{"2024.10.01", "2024.03.15", "2023.12.31"}, versions)
}

func TestVersioning_BuiltinCatalogOrdering(t *testing.T) {
	r := require.New(t)

	ubuntu := versioning.NewRegistry(calverUbuntu())
	uv := []string{"22.04", "22.10", "23.04"}
	ubuntu.SortDescending(uv)
	r.Equal([]string{"23.04", "22.10", "22.04"}, uv, "ubuntu YY.MM must compare month numerically")

	builds := versioning.NewRegistry(buildNumber())
	bv := []string{"1837", "1838", "1900"}
	builds.SortDescending(bv)
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
