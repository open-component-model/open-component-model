package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"cel.dev/cel-go/cel"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	ocmcel "ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/cel"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/cel/conversion"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// celInterruptCheckFrequency enables context cancellation checks during CEL
// evaluation. cel-go only observes the evaluation context when the interrupt
// check frequency is at least 1.
const celInterruptCheckFrequency = 100

// celCostLimit bounds the runtime cost of a single evaluation
//
// Roughly measured with unit tests. A 10_000 component graph would eat
// approximately half of this budget.
const celCostLimit = 1_000_000

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

	// component is the root component info when the OCI helpers are enabled
	// (see WithComponentInfo); nil otherwise.
	component *v1alpha1.ComponentInfo
	// extractSpec keeps the extraction expressions so that they can be
	// recompiled per component with that component's own repository.
	extractSpec *v1alpha1.Extract
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

	// perComponent caches the field programs compiled against a specific
	// component's repository (OCI helpers enabled). Filled lazily by fieldsFor.
	perComponent map[ComponentKey][]extractedField
}

// CompileOption customizes Compile.
type CompileOption func(*compileOptions)

type compileOptions struct {
	component *v1alpha1.ComponentInfo
}

// WithComponentInfo makes the OCI helper functions (toOCI and friends)
// available to extraction expressions, so by-value (localBlob) resources get
// a pullable address instead of only a digest.
//
// The expressions are validated once at compile time against this root
// component info. At projection time they are compiled again per component,
// with that component's own name and the repository it was resolved from
// (Graph.Repositories), because a localBlob lives in the repository of the
// component that declares it. A component without an entry in
// Graph.Repositories falls back to the root's repository specification.
func WithComponentInfo(component *v1alpha1.ComponentInfo) CompileOption {
	return func(o *compileOptions) { o.component = component }
}

// Compile compiles all selector and extraction expressions of spec once.
// Compilation errors are reported as *SelectorError or *ExtractError with the
// affected stage or field. An invalid extract mode combination is reported as
// an *ExtractError.
func Compile(ctx context.Context, spec *v1alpha1.DiscoverySpec, opts ...CompileOption) (*Query, error) {
	if spec == nil {
		return nil, fmt.Errorf("discovery spec must not be nil")
	}
	var o compileOptions
	for _, opt := range opts {
		opt(&o)
	}
	base, err := ocmcel.BaseEnv()
	if err != nil {
		return nil, fmt.Errorf("failed to load base CEL environment: %w", err)
	}
	// Extraction expressions see the OCI helpers only when a component info
	// is given; selectors keep the plain base environment.
	extractBase := base
	if o.component != nil {
		if extractBase, err = ocmcel.ComponentInfoEnv(o.component); err != nil {
			return nil, fmt.Errorf("failed to load component CEL environment: %w", err)
		}
	}

	selectorEnv, err := base.Extend(
		cel.Variable("identity", cel.MapType(cel.StringType, cel.StringType)),
		cel.Variable("labels", cel.DynType),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to extend base CEL environment: %w", err)
	}

	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	q := &Query{component: o.component, extractSpec: spec.Extract}
	if q.references, err = compileSelector(ctx, selectorEnv, StageReference, spec.ReferenceSelector); err != nil {
		return nil, err
	}
	if q.components, err = compileSelector(ctx, selectorEnv, StageComponent, spec.ComponentSelector); err != nil {
		return nil, err
	}
	if q.resources, err = compileSelector(ctx, selectorEnv, StageResource, spec.ResourceSelector); err != nil {
		return nil, err
	}
	if q.extract, err = compileExtract(ctx, extractBase, spec.Extract); err != nil {
		return nil, err
	}
	return q, nil
}

// compileSelector compiles a single selector. A nil or empty selector compiles
// to nil and matches everything, including the graph root.
func compileSelector(_ context.Context, env *cel.Env, stage string, sel *v1alpha1.Selector) (*compiledSelector, error) {
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
			return nil, selectorErrorf(stage, "failed to compile expression: %w", err)
		}
		cs.prog = prog
	}
	return cs, nil
}

func compileExtract(ctx context.Context, base *cel.Env, extract *v1alpha1.Extract) (*compiledExtract, error) {
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
			return nil, extractErrorf("", "failed to compile expression: %w", err)
		}
		return &compiledExtract{mode: extractExpression, expression: prog}, nil
	}
}

// fieldsFor returns the field programs to evaluate for the component key.
// Without OCI helpers (no component info) the programs compiled once by
// Compile are shared by every component. With OCI helpers the programs are
// compiled per component, bound to that component's name and the repository
// it was resolved from, and cached for the lifetime of the Query.
func (q *Query) fieldsFor(ctx context.Context, key ComponentKey, repositories map[ComponentKey]runtime.Typed) ([]extractedField, error) {
	if q.extract == nil {
		return nil, nil
	}
	if q.component == nil {
		return q.extract.fields, nil
	}
	if fields, ok := q.extract.perComponent[key]; ok {
		return fields, nil
	}

	info := &v1alpha1.ComponentInfo{
		Component:      key.Name,
		Version:        key.Version,
		RepositorySpec: q.component.RepositorySpec,
	}
	if spec, ok := repositories[key]; ok && spec != nil {
		raw, err := json.Marshal(spec)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal repository specification of component %s: %w", key, err)
		}
		info.RepositorySpec = &apiextensionsv1.JSON{Raw: raw}
	}

	env, err := ocmcel.ComponentInfoEnv(info)
	if err != nil {
		return nil, fmt.Errorf("failed to load component CEL environment for %s: %w", key, err)
	}
	var exprs map[string]string
	switch q.extract.mode {
	case extractByResources:
		env, err = env.Extend(cel.Variable("component", cel.DynType), cel.Variable("resource", cel.DynType))
		exprs = q.extractSpec.ByResources
	case extractByComponents:
		env, err = env.Extend(cel.Variable("component", cel.DynType))
		exprs = q.extractSpec.ByComponents
	default:
		return q.extract.fields, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to extend component CEL environment for %s: %w", key, err)
	}
	fields, err := compileFields(ctx, env, exprs)
	if err != nil {
		return nil, err
	}
	if q.extract.perComponent == nil {
		q.extract.perComponent = map[ComponentKey][]extractedField{}
	}
	q.extract.perComponent[key] = fields
	return fields, nil
}

// compileFields compiles one CEL expression per map value. Field names are
// processed in lexicographic order so compilation errors are deterministic.
func compileFields(_ context.Context, env *cel.Env, exprs map[string]string) ([]extractedField, error) {
	fields := make([]extractedField, 0, len(exprs))
	for _, name := range slices.Sorted(maps.Keys(exprs)) {
		prog, err := compileProgram(env, exprs[name])
		if err != nil {
			return nil, extractErrorf(name, "failed to compile expression: %w", err)
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
	prog, err := env.Program(ast,
		cel.InterruptCheckFrequency(celInterruptCheckFrequency),
		cel.CostLimit(celCostLimit))
	if err != nil {
		return nil, fmt.Errorf("failed to build CEL program %q: %w", expr, err)
	}
	return prog, nil
}

// matches reports whether the element with the given identity and labels
// survives the selector. All clauses are ANDed. A missing field, key or index
// access in the selector expression is a nonmatch; a nonboolean result or any
// other evaluation error is a *SelectorError.
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
	val, _, err := s.prog.ContextEval(ctx, map[string]any{
		"identity": map[string]string(identity),
		"labels":   labels,
	})
	missing, failure := evalResult(ctx, val, err)
	if missing {
		return false, nil
	}
	if failure != nil {
		return false, selectorErrorf(s.stage, "failed to evaluate expression: %w", failure)
	}
	native, err := conversion.GoNativeType(val)
	if err != nil {
		return false, selectorErrorf(s.stage, "failed to convert expression result: %w", err)
	}
	b, ok := native.(bool)
	if !ok {
		return false, selectorErrorf(s.stage, "expression must evaluate to a boolean, got %T", native)
	}
	return b, nil
}
