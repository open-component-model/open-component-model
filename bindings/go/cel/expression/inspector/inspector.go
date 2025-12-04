package ast

import (
	"fmt"
	"strings"

	"github.com/google/cel-go/cel"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"

	"ocm.software/open-component-model/bindings/go/cel/expression/fieldpath"
)

type ResourceDependency struct {
	ID   string
	Path fieldpath.Path
}

type FunctionCall struct {
	Name      string
	Arguments []string
}

type UnknownResource struct {
	ID   string
	Path fieldpath.Path
}

type UnknownFunction struct {
	Name string
}

type ExpressionInspection struct {
	ResourceDependencies []ResourceDependency
	FunctionCalls        []FunctionCall
	UnknownResources     []UnknownResource
	UnknownFunctions     []UnknownFunction
}

// Merge in another inspection.
func (e *ExpressionInspection) Merge(other ExpressionInspection) {
	if len(other.ResourceDependencies) != 0 {
		e.ResourceDependencies = append(e.ResourceDependencies, other.ResourceDependencies...)
	}
	if len(other.FunctionCalls) != 0 {
		e.FunctionCalls = append(e.FunctionCalls, other.FunctionCalls...)
	}
	if len(other.UnknownResources) != 0 {
		e.UnknownResources = append(e.UnknownResources, other.UnknownResources...)
	}
	if len(other.UnknownFunctions) != 0 {
		e.UnknownFunctions = append(e.UnknownFunctions, other.UnknownFunctions...)
	}
}

// -----------------------------------------------------------------------------
// INSPECTOR
// -----------------------------------------------------------------------------

type Inspector struct {
	env       *cel.Env
	resources map[string]struct{}
	functions map[string]struct{}
	loopVars  map[string]struct{}
}

func NewInspectorWithEnv(env *cel.Env, resources []string, functions []string) *Inspector {
	rs := make(map[string]struct{}, len(resources))
	for _, r := range resources {
		rs[r] = struct{}{}
	}
	fns := make(map[string]struct{}, len(functions))
	for _, fn := range functions {
		fns[fn] = struct{}{}
	}
	return &Inspector{
		env:       env,
		resources: rs,
		functions: fns,
		loopVars:  make(map[string]struct{}),
	}
}

// -----------------------------------------------------------------------------
// ENTRYPOINT
// -----------------------------------------------------------------------------

func (a *Inspector) Inspect(expr string) (ExpressionInspection, error) {
	ast, iss := a.env.Parse(expr)
	if iss.Err() != nil {
		return ExpressionInspection{}, fmt.Errorf("failed to parse expression: %w", iss.Err())
	}

	parsed, err := cel.AstToParsedExpr(ast)
	if err != nil {
		return ExpressionInspection{}, fmt.Errorf("failed to convert to ParsedExpr: %w", err)
	}

	return a.inspect(parsed.GetExpr(), nil), nil
}

// -----------------------------------------------------------------------------
// AST WALKER
// -----------------------------------------------------------------------------

func (a *Inspector) inspect(e *exprpb.Expr, suffix fieldpath.Path) ExpressionInspection {
	if e == nil {
		return ExpressionInspection{}
	}

	switch node := e.GetExprKind().(type) {
	case *exprpb.Expr_SelectExpr:
		// Build path backwards: field.(suffix)
		field := node.SelectExpr.GetField()
		newSuffix := fieldpath.New().AddNamed(field).Add(suffix...)
		return a.inspect(node.SelectExpr.GetOperand(), newSuffix)

	case *exprpb.Expr_IdentExpr:
		return a.inspectIdent(node.IdentExpr, suffix)

	case *exprpb.Expr_CallExpr:
		return a.inspectCall(node.CallExpr, suffix)

	case *exprpb.Expr_ComprehensionExpr:
		return a.inspectComprehension(node.ComprehensionExpr, suffix)

	default:
		return ExpressionInspection{}
	}
}

// -----------------------------------------------------------------------------
// IDENT INSPECTION
// -----------------------------------------------------------------------------

func (a *Inspector) inspectIdent(id *exprpb.Expr_Ident, suffix fieldpath.Path) ExpressionInspection {
	name := id.GetName()

	// Loop variables never contribute resource usage.
	if _, ok := a.loopVars[name]; ok {
		return ExpressionInspection{}
	}

	// Known resource
	if _, ok := a.resources[name]; ok {
		return ExpressionInspection{
			ResourceDependencies: []ResourceDependency{{
				ID:   name,
				Path: fieldpath.New().AddNamed(name).Add(suffix...),
			}},
		}
	}

	// Skip internal CEL identifiers
	if isInternalIdentifier(name) {
		return ExpressionInspection{}
	}

	// Unknown resource
	return ExpressionInspection{
		UnknownResources: []UnknownResource{{
			ID:   name,
			Path: fieldpath.New(fieldpath.NamedSegment(name)).Add(suffix...),
		}},
	}
}

// -----------------------------------------------------------------------------
// FUNCTION CALL INSPECTION
// -----------------------------------------------------------------------------

func (a *Inspector) inspectCall(c *exprpb.Expr_Call, suffix fieldpath.Path) ExpressionInspection {
	out := ExpressionInspection{}

	// Inspect arguments first
	for _, arg := range c.GetArgs() {
		out.Merge(a.inspect(arg, nil))
	}

	// Namespaced: ident.fn()
	if c.GetTarget() != nil {
		targetStr := a.exprToString(c.GetTarget())
		fullName := targetStr + "." + c.GetFunction()

		out.Merge(a.inspect(c.GetTarget(), suffix))

		// Known namespaced function?
		if _, ok := a.functions[fullName]; ok {
			out.FunctionCalls = append(out.FunctionCalls, FunctionCall{
				Name:      fullName,
				Arguments: inspectArgsAsStrings(a, c.GetArgs()),
			})
			return out
		}

		// Unknown namespaced function?
		if !a.env.HasFunction(c.GetFunction()) {
			out.UnknownFunctions = append(out.UnknownFunctions, UnknownFunction{Name: fullName})
		}

		// Always record chained call
		out.FunctionCalls = append(out.FunctionCalls, FunctionCall{
			Name: fullName,
		})
		return out
	}

	// Non-namespaced call
	if _, ok := a.functions[c.GetFunction()]; ok {
		out.FunctionCalls = append(out.FunctionCalls, FunctionCall{
			Name:      c.GetFunction(),
			Arguments: inspectArgsAsStrings(a, c.GetArgs()),
		})
	} else if !a.env.HasFunction(c.GetFunction()) {
		out.UnknownFunctions = append(out.UnknownFunctions, UnknownFunction{Name: c.GetFunction()})
	}

	return out
}

// -----------------------------------------------------------------------------
// COMPREHENSION INSPECTION
// -----------------------------------------------------------------------------

func (a *Inspector) inspectComprehension(c *exprpb.Expr_Comprehension, suffix fieldpath.Path) ExpressionInspection {
	out := ExpressionInspection{}

	a.loopVars[c.GetIterVar()] = struct{}{}
	defer delete(a.loopVars, c.GetIterVar())

	out.Merge(a.inspect(c.GetIterRange(), suffix))
	out.Merge(a.inspect(c.GetLoopCondition(), nil))
	out.Merge(a.inspect(c.GetLoopStep(), nil))
	out.Merge(a.inspect(c.GetResult(), nil))

	// Determine operation type (filter or map)
	if c.GetLoopStep() == nil {
		out.FunctionCalls = append(out.FunctionCalls, FunctionCall{
			Name: "filter",
			Arguments: []string{
				a.exprToString(c.GetIterRange()),
				a.exprToString(c.GetLoopCondition()),
				a.exprToString(c.GetResult()),
			},
		})
	} else {
		out.FunctionCalls = append(out.FunctionCalls, FunctionCall{
			Name: "map",
			Arguments: []string{
				a.exprToString(c.GetIterRange()),
				a.exprToString(c.GetLoopStep()),
				a.exprToString(c.GetResult()),
			},
		})
	}

	return out
}

// -----------------------------------------------------------------------------
// STRINGIFICATION HELPERS
// -----------------------------------------------------------------------------

func inspectArgsAsStrings(a *Inspector, args []*exprpb.Expr) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		out = append(out, a.exprToString(arg))
	}
	return out
}

func (a *Inspector) exprToString(e *exprpb.Expr) string {
	if e == nil {
		return "<nil>"
	}

	switch node := e.GetExprKind().(type) {
	case *exprpb.Expr_ConstExpr:
		return constToString(node.ConstExpr)

	case *exprpb.Expr_IdentExpr:
		return node.IdentExpr.GetName()

	case *exprpb.Expr_SelectExpr:
		return fmt.Sprintf("%s.%s", a.exprToString(node.SelectExpr.GetOperand()), node.SelectExpr.GetField())

	case *exprpb.Expr_CallExpr:
		return a.callToString(node.CallExpr)

	case *exprpb.Expr_ListExpr:
		items := make([]string, len(node.ListExpr.GetElements()))
		for i, el := range node.ListExpr.GetElements() {
			items[i] = a.exprToString(el)
		}
		return "[" + strings.Join(items, ", ") + "]"

	case *exprpb.Expr_StructExpr:
		return a.structToString(node.StructExpr)

	default:
		return fmt.Sprintf("<unknown %T>", node)
	}
}

func constToString(c *exprpb.Constant) string {
	switch v := c.GetConstantKind().(type) {
	case *exprpb.Constant_BoolValue:
		return fmt.Sprintf("%v", v.BoolValue)
	case *exprpb.Constant_BytesValue:
		return fmt.Sprintf("b\"%s\"", v.BytesValue)
	case *exprpb.Constant_DoubleValue:
		return fmt.Sprintf("%v", v.DoubleValue)
	case *exprpb.Constant_Int64Value:
		return fmt.Sprintf("%v", v.Int64Value)
	case *exprpb.Constant_StringValue:
		return fmt.Sprintf("%q", v.StringValue)
	case *exprpb.Constant_Uint64Value:
		return fmt.Sprintf("%vu", v.Uint64Value)
	case *exprpb.Constant_NullValue:
		return "null"
	default:
		return "<unknown const>"
	}
}

func (a *Inspector) callToString(c *exprpb.Expr_Call) string {
	args := inspectArgsAsStrings(a, c.GetArgs())

	// Operators like _+_
	if isOperatorCall(c.GetFunction()) && len(args) >= 2 {
		return operatorToString(c.GetFunction(), args)
	}

	if c.GetTarget() != nil {
		return fmt.Sprintf("%s.%s(%s)",
			a.exprToString(c.GetTarget()),
			c.GetFunction(),
			strings.Join(args, ", "),
		)
	}

	return fmt.Sprintf("%s(%s)", c.GetFunction(), strings.Join(args, ", "))
}

func isOperatorCall(fn string) bool {
	return strings.HasPrefix(fn, "_") && strings.HasSuffix(fn, "_")
}

func operatorToString(op string, args []string) string {
	trim := strings.Trim(op, "_")
	switch op {
	case "_?_:_":
		if len(args) == 3 {
			return fmt.Sprintf("(%s ? %s : %s)", args[0], args[1], args[2])
		}
	case "_[_]":
		if len(args) == 2 {
			return fmt.Sprintf("%s[%s]", args[0], args[1])
		}
	}
	if len(args) == 2 {
		return fmt.Sprintf("(%s %s %s)", args[0], trim, args[1])
	}
	return op + "(" + strings.Join(args, ", ") + ")"
}

func (a *Inspector) structToString(s *exprpb.Expr_CreateStruct) string {
	entries := make([]string, len(s.GetEntries()))
	for i, entry := range s.GetEntries() {
		val := a.exprToString(entry.GetValue())
		switch {
		case entry.GetFieldKey() != "":
			entries[i] = fmt.Sprintf("%s: %s", entry.GetFieldKey(), val)
		case entry.GetMapKey() != nil:
			entries[i] = fmt.Sprintf("%s: %s", a.exprToString(entry.GetMapKey()), val)
		}
	}

	if s.GetMessageName() != "" {
		return fmt.Sprintf("%s{%s}", s.GetMessageName(), strings.Join(entries, ", "))
	}
	return fmt.Sprintf("{%s}", strings.Join(entries, ", "))
}

func isInternalIdentifier(name string) bool {
	return name == "@result" || strings.HasPrefix(name, "$$")
}
