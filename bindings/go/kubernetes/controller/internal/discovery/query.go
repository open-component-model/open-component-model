package discovery

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/google/cel-go/cel"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	ocmcel "ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/cel"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/cel/conversion"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// celInterruptCheckFrequency enables context cancellation checks during CEL
// evaluation. cel-go only observes the evaluation context when the interrupt
// check frequency is at least 1.
const celInterruptCheckFrequency = 100

type extractMode int

const (
	extractByResources extractMode = iota
	extractByComponents
	extractExpression
)

// Query is a compiled Discovery selector/extraction pipeline. Expressions are
// compiled once per reconcile and then evaluated for every element.
type Query struct {
	references *compiledSelector
	components *compiledSelector
	resources  *compiledSelector
	extract    *compiledExtract
}

type compiledSelector struct {
	stage         string
	matchIdentity map[string]string
	matchLabels   map[string]string
	prog          cel.Program // nil when the selector expression is empty
}

type extractedField struct {
	name string
	prog cel.Program
}

type compiledExtract struct {
	mode       extractMode
	fields     []extractedField // sorted by field name; byResources / byComponents modes
	expression cel.Program      // expression mode
}

// Compile compiles all selector and extraction expressions of spec once.
// Compilation errors are reported as *SelectorError or *ExtractError with the
// affected stage or field. An invalid extract mode combination is reported as
// an *ExtractError.
func Compile(ctx context.Context, spec *v1alpha1.DiscoverySpec) (*Query, error) {
	if spec == nil {
		return nil, fmt.Errorf("discovery spec must not be nil")
	}
	base, err := ocmcel.BaseEnv()
	if err != nil {
		return nil, fmt.Errorf("failed to load base CEL environment: %w", err)
	}

	selectorEnv, err := base.Extend(
		cel.Variable("identity", cel.MapType(cel.StringType, cel.StringType)),
		cel.Variable("labels", cel.DynType),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to extend base CEL environment: %w", err)
	}

	q := &Query{}
	if q.references, err = compileSelector(ctx, selectorEnv, StageReference, spec.ReferenceSelector); err != nil {
		return nil, err
	}
	if q.components, err = compileSelector(ctx, selectorEnv, StageComponent, spec.ComponentSelector); err != nil {
		return nil, err
	}
	if q.resources, err = compileSelector(ctx, selectorEnv, StageResource, spec.ResourceSelector); err != nil {
		return nil, err
	}
	if q.extract, err = compileExtract(ctx, base, spec.Extract); err != nil {
		return nil, err
	}
	return q, nil
}

// compileSelector compiles a single selector. A nil or empty selector compiles
// to nil and matches everything, including the graph root.
func compileSelector(ctx context.Context, env *cel.Env, stage string, sel *v1alpha1.Selector) (*compiledSelector, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if sel == nil || (len(sel.MatchIdentity) == 0 && len(sel.MatchLabels) == 0 && sel.Expression == "") {
		return nil, nil
	}
	cs := &compiledSelector{
		stage:         stage,
		matchIdentity: sel.MatchIdentity,
		matchLabels:   sel.MatchLabels,
	}
	if sel.Expression != "" {
		prog, err := compileProgram(env, sel.Expression)
		if err != nil {
			return nil, selectorErrorf(stage, "failed to compile expression: %s", err)
		}
		cs.prog = prog
	}
	return cs, nil
}

func compileExtract(ctx context.Context, base *cel.Env, extract *v1alpha1.Extract) (*compiledExtract, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if extract == nil {
		return nil, nil
	}
	modes := 0
	for _, present := range []bool{extract.ByResources != nil, extract.ByComponents != nil, extract.Expression != ""} {
		if present {
			modes++
		}
	}
	if modes != 1 {
		return nil, extractErrorf("", "exactly one of byResources, byComponents, or expression must be specified")
	}

	switch {
	case extract.ByResources != nil:
		env, err := base.Extend(cel.Variable("component", cel.DynType), cel.Variable("resource", cel.DynType))
		if err != nil {
			return nil, fmt.Errorf("failed to extend base CEL environment: %w", err)
		}
		fields, err := compileFields(ctx, env, extract.ByResources)
		if err != nil {
			return nil, err
		}
		return &compiledExtract{mode: extractByResources, fields: fields}, nil
	case extract.ByComponents != nil:
		env, err := base.Extend(cel.Variable("component", cel.DynType))
		if err != nil {
			return nil, fmt.Errorf("failed to extend base CEL environment: %w", err)
		}
		fields, err := compileFields(ctx, env, extract.ByComponents)
		if err != nil {
			return nil, err
		}
		return &compiledExtract{mode: extractByComponents, fields: fields}, nil
	default:
		env, err := base.Extend(cel.Variable("components", cel.DynType))
		if err != nil {
			return nil, fmt.Errorf("failed to extend base CEL environment: %w", err)
		}
		prog, err := compileProgram(env, extract.Expression)
		if err != nil {
			return nil, extractErrorf("", "failed to compile expression: %s", err)
		}
		return &compiledExtract{mode: extractExpression, expression: prog}, nil
	}
}

// compileFields compiles one CEL expression per map value. Field names are
// processed in lexicographic order so compilation errors are deterministic.
func compileFields(ctx context.Context, env *cel.Env, exprs map[string]string) ([]extractedField, error) {
	fields := make([]extractedField, 0, len(exprs))
	for _, name := range slices.Sorted(maps.Keys(exprs)) {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		prog, err := compileProgram(env, exprs[name])
		if err != nil {
			return nil, extractErrorf(name, "failed to compile expression: %s", err)
		}
		fields = append(fields, extractedField{name: name, prog: prog})
	}
	return fields, nil
}

func compileProgram(env *cel.Env, expr string) (cel.Program, error) {
	ast, issues := env.Compile(expr)
	if issues.Err() != nil {
		return nil, issues.Err()
	}
	prog, err := env.Program(ast, cel.InterruptCheckFrequency(celInterruptCheckFrequency))
	if err != nil {
		return nil, fmt.Errorf("failed to build CEL program %q: %w", expr, err)
	}
	return prog, nil
}

// matches reports whether the element with the given identity and labels
// survives the selector. All clauses are ANDed. A missing field or key access
// in the selector expression is a nonmatch; a nonboolean result or any other
// evaluation error is a *SelectorError.
func (s *compiledSelector) matches(ctx context.Context, identity runtime.Identity, labels map[string]any) (bool, error) {
	if s == nil {
		return true, nil
	}
	if !matchIdentity(s.matchIdentity, identity) {
		return false, nil
	}
	if !matchLabels(s.matchLabels, labels) {
		return false, nil
	}
	if s.prog == nil {
		return true, nil
	}
	if err := checkContext(ctx); err != nil {
		return false, err
	}
	if labels == nil {
		labels = map[string]any{}
	}
	val, _, err := s.prog.ContextEval(ctx, map[string]any{
		"identity": map[string]string(identity),
		"labels":   labels,
	})
	if err := checkContext(ctx); err != nil {
		return false, err
	}
	if missing, failure := evalResult(val, err); missing {
		return false, nil
	} else if failure != nil {
		return false, selectorErrorf(s.stage, "failed to evaluate expression: %s", failure)
	}
	native, err := conversion.GoNativeType(val)
	if err != nil {
		return false, selectorErrorf(s.stage, "failed to convert expression result: %s", err)
	}
	b, ok := native.(bool)
	if !ok {
		return false, selectorErrorf(s.stage, "expression must evaluate to a boolean, got %T", native)
	}
	return b, nil
}
