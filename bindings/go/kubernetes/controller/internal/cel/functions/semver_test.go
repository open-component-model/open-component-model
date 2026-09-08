package functions_test

import (
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/stretchr/testify/require"

	ocmcel "ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/cel"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/cel/functions"
)

func TestSemverCheck(t *testing.T) {
	r := require.New(t)
	env, err := cel.NewEnv(functions.SemverCheck(), cel.Variable("identity", cel.MapType(cel.StringType, cel.StringType)))
	r.NoError(err)

	eval := func(expr string, vars map[string]any) (ref.Val, error) {
		ast, issues := env.Compile(expr)
		if issues.Err() != nil {
			t.Fatalf("compile %q: %s", expr, issues.Err())
		}
		prog, err := env.Program(ast)
		r.NoError(err)
		val, _, err := prog.ContextEval(t.Context(), vars)
		return val, err
	}

	identity := map[string]string{"name": "x", "version": "2.7.0"}

	t.Run("constraint satisfied", func(t *testing.T) {
		val, err := eval(`semverCheck(identity.version, ">=2.7.0, <2.10.0")`, map[string]any{"identity": identity})
		r.NoError(err)
		r.Equal(types.True, val)
	})

	t.Run("constraint not satisfied", func(t *testing.T) {
		val, err := eval(`semverCheck(identity.version, "<2.0.0")`, map[string]any{"identity": identity})
		r.NoError(err)
		r.Equal(types.False, val)
	})

	t.Run("v-prefixed version accepted", func(t *testing.T) {
		val, err := eval(`semverCheck("v2.7.0", ">=2.0.0")`, nil)
		r.NoError(err)
		r.Equal(types.True, val)
	})

	t.Run("invalid version is an error", func(t *testing.T) {
		_, err := eval(`semverCheck("not-a-version", ">=2.0.0")`, nil)
		r.Error(err)
		r.Contains(err.Error(), "invalid version")
		r.NotContains(err.Error(), "no such key")
	})

	t.Run("invalid constraint is an error", func(t *testing.T) {
		_, err := eval(`semverCheck("2.7.0", "garbage-constraint")`, nil)
		r.Error(err)
		r.Contains(err.Error(), "invalid constraint")
	})
}

func TestBaseEnvHasSemverCheck(t *testing.T) {
	r := require.New(t)
	env, err := ocmcel.BaseEnv()
	r.NoError(err)
	ast, issues := env.Compile(`semverCheck("1.0.0", ">=1.0.0")`)
	r.NoError(issues.Err())
	r.Equal(cel.BoolType, ast.OutputType())
}
