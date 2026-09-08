package functions

import (
	"github.com/Masterminds/semver/v3"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

const SemverCheckFunctionName = "semverCheck"

// SemverCheck returns a CEL environment option that registers the "semverCheck" function.
// It takes a version and a constraint string and returns whether the version satisfies the
// constraint, e.g. semverCheck(identity.version, ">=2.7.0, <2.10.0").
// Invalid versions or constraints produce a CEL error instead of a false result.
func SemverCheck() cel.EnvOption {
	return cel.Function(
		SemverCheckFunctionName,
		cel.Overload(
			"semverCheck_string_string",
			[]*cel.Type{cel.StringType, cel.StringType},
			cel.BoolType,
		),
		cel.SingletonBinaryBinding(BindingSemverCheck()),
	)
}

// BindingSemverCheck is the implementation of the semverCheck function.
func BindingSemverCheck() func(lhs, rhs ref.Val) ref.Val {
	return func(lhs, rhs ref.Val) ref.Val {
		version, ok := lhs.Value().(string)
		if !ok {
			return types.NewErr("semverCheck: first argument must be a version string, got %T", lhs.Value())
		}
		constraint, ok := rhs.Value().(string)
		if !ok {
			return types.NewErr("semverCheck: second argument must be a constraint string, got %T", rhs.Value())
		}
		v, err := semver.NewVersion(version)
		if err != nil {
			return types.NewErr("semverCheck: invalid version %q: %s", version, err)
		}
		c, err := semver.NewConstraint(constraint)
		if err != nil {
			return types.NewErr("semverCheck: invalid constraint %q: %s", constraint, err)
		}
		return types.Bool(c.Check(v))
	}
}
