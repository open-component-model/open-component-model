package discovery

import (
	"context"
	"testing"
	"time"

	"cel.dev/cel-go/cel"
	"github.com/stretchr/testify/require"

	ocmcel "ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/cel"
)

// evalExpr compiles and evaluates expr against act using the real base
// environment, so the assertions below pin cel-go's actual error messages
// rather than hand-written strings.
func evalExpr(t *testing.T, ctx context.Context, expr string, act map[string]any) (bool, error) {
	t.Helper()
	base, err := ocmcel.BaseEnv()
	require.NoError(t, err)
	env, err := base.Extend(
		cel.Variable("components", cel.DynType),
		cel.Variable("component", cel.DynType),
	)
	require.NoError(t, err)
	ast, issues := env.Compile(expr)
	require.NoError(t, issues.Err())
	prog, err := env.Program(ast, cel.InterruptCheckFrequency(celInterruptCheckFrequency))
	require.NoError(t, err)
	val, _, evalErr := prog.ContextEval(ctx, act)

	return evalResult(ctx, val, evalErr)
}

// TestIsMissingAccessShapes drives every shape cel-go's attribute resolution
// can produce through a real evaluation. All three come from the same
// unexported *resolutionError, so any one of them missing from
// missingAccessPrefixes silently turns a soft omit into a terminal stall.
func TestIsMissingAccessShapes(t *testing.T) {
	act := map[string]any{
		"components": []any{map[string]any{"component": map[string]any{"name": "a"}}},
		"component":  map[string]any{"name": "a", "resources": nil},
	}

	for _, tc := range []struct {
		name    string
		expr    string
		message string
	}{
		{"missing map key", `component.absent`, "no such key:"},
		{"missing nested key", `components[0].component.absent.deeper`, "no such key:"},
		{"index out of bounds", `components[5]`, "index out of bounds:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			missing, cause := evalExpr(t, t.Context(), tc.expr, act)
			r.Error(cause)
			r.Contains(cause.Error(), tc.message, "cel-go message changed; update missingAccessPrefixes")
			r.True(missing, "%s must be a missing access, not a semantic failure", tc.expr)
		})
	}
}

// TestIsMissingAccessRejectsGenuineErrors pins the other half of the contract:
// a real type error must stay a failure. size(null) is the common trap, since a
// v2 descriptor serialises an absent resource list as null rather than omitting
// it.
func TestIsMissingAccessRejectsGenuineErrors(t *testing.T) {
	r := require.New(t)
	missing, cause := evalExpr(t, t.Context(), `size(component.resources)`, map[string]any{
		"components": []any{},
		"component":  map[string]any{"name": "a", "resources": nil},
	})
	r.Error(cause)
	r.Contains(cause.Error(), "no such overload")
	r.False(missing, "no such overload must never be treated as a missing access")
}

// TestEvalResultCatchesMidEvaluationCancellation covers the case the loop-head
// context checks cannot: the context is live on entry and dies during the
// evaluation. evalResult must report it rather than let the pipeline carry on
// against a dead context until the next loop head.
func TestEvalResultCatchesMidEvaluationCancellation(t *testing.T) {
	r := require.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	r.NoError(ctx.Err(), "context must be live on entry, or the test proves nothing")

	expr := `size(lists.range(1000000).map(x, x*2).map(x, x*2).map(x, x*2).map(x, x*2).map(x, x*2).map(x, x*2))`
	missing, cause := evalExpr(t, ctx, expr, map[string]any{"components": []any{}, "component": map[string]any{}})

	r.False(missing, "a cancellation is not a missing access")
	r.ErrorIs(cause, context.DeadlineExceeded)
}
