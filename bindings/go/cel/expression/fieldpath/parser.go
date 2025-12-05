package fieldpath

import (
	"fmt"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/ast"
)

var pathParsingEnvironment *cel.Env

func init() {
	// Build CEL env without macros as we only want field path analysis.
	var err error
	if pathParsingEnvironment, err = cel.NewEnv(cel.ClearMacros()); err != nil {
		panic(err)
	}
}

func MustParse(path string) Path {
	segments, err := Parse(path)
	if err != nil {
		panic(err)
	}
	return segments
}

// Parse parses a path string into segments. Assumes dictionary
// access is always quoted.
func Parse(path string) (Path, error) {
	parsed, iss := pathParsingEnvironment.Parse(path)
	if iss != nil && iss.Err() != nil {
		return nil, iss.Err()
	}
	nativeAST := parsed.NativeRep()
	return exprToSegments(nativeAST.Expr())
}

// exprToSegments walks a CEL expr recursively.
func exprToSegments(e ast.Expr) ([]Segment, error) {
	switch e.Kind() {
	// "root" identifier:  spec
	case ast.IdentKind:
		return []Segment{{Name: e.AsIdent()}}, nil
	// field selection:   spec.items
	case ast.SelectKind:
		sel := e.AsSelect()
		base, err := exprToSegments(sel.Operand())
		if err != nil {
			return nil, err
		}
		return append(base, Segment{Name: sel.FieldName()}), nil
	// index or member-call:  x[0], x["key"]
	case ast.CallKind:
		call := e.AsCall()

		// Want only index operator: container[expr] is parsed as function "_[_]"
		if call.FunctionName() != "_[_]" {
			return nil, fmt.Errorf("unsupported call function %q", call.FunctionName())
		}

		// Member function is possible, but array/dict indexing is non-member:
		// _[_](container, key)
		args := call.Args()
		if len(args) != 2 {
			return nil, fmt.Errorf("index operator argument count mismatch")
		}

		container := args[0]
		keyExpr := args[1]

		base, err := exprToSegments(container)
		if err != nil {
			return nil, err
		}

		seg, err := segmentFromIndexArg(keyExpr)
		if err != nil {
			return nil, err
		}

		return append(base, seg), nil
	case ast.LiteralKind:
		// Handle literal root, e.g. "3dmatrix"
		lit := e.AsLiteral()
		if v, ok := lit.Value().(string); ok {
			return []Segment{{Name: v}}, nil
		}
		return nil, fmt.Errorf("unsupported literal root %T", lit.Value())
	case ast.ListKind:
		// Handle literal list root, e.g. ["my.field"]
		list := e.AsList()
		segments := make([]Segment, 0, len(list.Elements()))
		for _, expr := range list.Elements() {
			expr, err := exprToSegments(expr)
			if err != nil {
				return nil, err
			}
			segments = append(segments, expr...)
		}
		return segments, nil
	default:
		return nil, fmt.Errorf("unsupported expression kind %v", e.Kind())
	}
}

// interpret key expression: must be literal string or literal int
func segmentFromIndexArg(e ast.Expr) (Segment, error) {
	switch e.Kind() {
	case ast.LiteralKind:
		lit := e.AsLiteral()
		switch v := lit.Value().(type) {
		case string:
			return Segment{Name: v}, nil
		case int64:
			i := int(v)
			return Segment{Index: &i}, nil
		default:
			return Segment{}, fmt.Errorf("unsupported index literal %T", v)
		}
	default:
		return Segment{}, fmt.Errorf("unsupported index expression kind %v", e.Kind())
	}
}
