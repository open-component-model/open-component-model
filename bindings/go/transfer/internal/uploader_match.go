package internal

import (
	"fmt"
	"reflect"
	"strconv"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
	"cel.dev/cel-go/ext"

	ocifunctions "ocm.software/open-component-model/bindings/go/oci/cel/functions"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	graphenv "ocm.software/open-component-model/bindings/go/transform/graph/env"
)

const (
	// targetAlias is the uploader alias for the transfer target. It is rewritten to a map
	// literal holding the target's type and its location fields.
	targetAlias = "target"

	isOCIManifestFunctionName = "isOCIManifest"
	isTypeFunctionName        = "isType"
)

// EnvOptions are the CEL functions the transfer graph offers beyond the graph's base
// environment: toOCI() exactly as the controller offers it (OCI image accesses), the
// string extensions (split, join, ...) used to compose image references, and
// isOCIManifest() and <access>.isType() for uploader match expressions.
func EnvOptions() []cel.EnvOption {
	return []cel.EnvOption{ocifunctions.ToOCI(), ext.Strings(), isOCIManifestFunction(), isTypeFunction()}
}

// isOCIManifestFunction declares isOCIManifest(string) bool: whether a media type is an
// OCI image manifest or index, or a Docker manifest or manifest list.
func isOCIManifestFunction() cel.EnvOption {
	return cel.Function(isOCIManifestFunctionName,
		cel.Overload(isOCIManifestFunctionName+"_string", []*cel.Type{cel.StringType}, cel.BoolType,
			cel.UnaryBinding(func(v ref.Val) ref.Val {
				s, ok := v.Value().(string)
				if !ok {
					return types.NewErr("%s() expects a string, got %T", isOCIManifestFunctionName, v.Value())
				}
				return types.Bool(isOCICompliantManifest(s))
			}),
		),
	)
}

// isTypeFunction declares <access>.isType(string) and <access>.isType(list(string)):
// whether the access's type is (one of) the given types, with aliases resolved through
// the transfer access scheme. An unversioned argument matches any version.
func isTypeFunction() cel.EnvOption {
	return cel.Function(isTypeFunctionName,
		cel.MemberOverload("isType_dyn_string", []*cel.Type{cel.DynType, cel.StringType}, cel.BoolType, cel.BinaryBinding(bindIsType)),
		cel.MemberOverload("isType_dyn_list_string", []*cel.Type{cel.DynType, cel.ListType(cel.StringType)}, cel.BoolType, cel.BinaryBinding(bindIsType)),
	)
}

// bindIsType implements isType: lhs is an access map with a string type, rhs one type
// string or a list of them.
func bindIsType(lhs, rhs ref.Val) ref.Val {
	native, err := lhs.ConvertToNative(reflect.TypeFor[map[string]any]())
	if err != nil {
		return types.NewErr("%s() expects an access with a type, got %T", isTypeFunctionName, lhs.Value())
	}
	typ, _ := native.(map[string]any)["type"].(string)
	if typ == "" {
		return types.NewErr("%s() expects an access with a type, got %T", isTypeFunctionName, lhs.Value())
	}
	accessType, err := runtime.TypeFromString(typ)
	if err != nil {
		return types.WrapErr(err)
	}

	var wants []string
	if s, ok := rhs.(types.String); ok {
		wants = []string{string(s)}
	} else {
		list, err := rhs.ConvertToNative(reflect.TypeFor[[]string]())
		if err != nil {
			return types.WrapErr(err)
		}
		wants = list.([]string)
	}
	for _, s := range wants {
		if s == "" {
			return types.NewErr("%s(): empty type", isTypeFunctionName)
		}
		want, err := runtime.TypeFromString(s)
		if err != nil {
			return types.NewErr("%s(): invalid type %q: %v", isTypeFunctionName, s, err)
		}
		if accessTypeIs(accessType, want) {
			return types.True
		}
	}
	return types.False
}

// accessTypeIs reports whether access is want after resolving both through scheme:
// same canonical name for an unversioned want, same canonical type otherwise.
// Unregistered types resolve to themselves.
func accessTypeIs(access, want runtime.Type) bool {
	a, _ := scheme.ResolveCanonicalType(access)
	w, _ := scheme.ResolveCanonicalType(want)
	if want.Version == "" {
		return a.Name == w.Name
	}
	return a.Equal(w)
}

// uploaderEnv lazily builds the CEL environment uploader expressions (match and the
// OCI imageReference) are evaluated in while the graph is built: the component's
// descriptor environment node plus [EnvOptions], i.e. what the graph evaluates templates
// against at runtime.
type uploaderEnv struct {
	baseID string
	node   any
	env    *cel.Env
}

func (e *uploaderEnv) get() (*cel.Env, error) {
	if e.env != nil {
		return e.env, nil
	}
	builder, err := graphenv.NewEnvBuilder(map[string]any{e.baseID: e.node})
	if err != nil {
		return nil, fmt.Errorf("cannot build CEL environment: %w", err)
	}
	builder.RegisterEnvOption(EnvOptions()...)
	env, _, err := builder.CurrentEnv()
	if err != nil {
		return nil, fmt.Errorf("cannot build CEL environment: %w", err)
	}
	e.env = env
	return env, nil
}

// program compiles expr in the uploader environment.
func (e *uploaderEnv) program(expr string) (cel.Program, error) {
	celEnv, err := e.get()
	if err != nil {
		return nil, err
	}
	ast, issues := celEnv.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}
	return celEnv.Program(ast)
}

// targetLiteral returns the CEL map literal the target alias is rewritten to: an OCI
// registry has type, baseUrl and subPath; a CTF archive has type and filePath.
func targetLiteral(toSpec runtime.Typed) (string, error) {
	repo, err := convertToConcreteRepo(toSpec)
	if err != nil {
		return "", err
	}
	switch r := repo.(type) {
	case *oci.Repository:
		return fmt.Sprintf("{%q: %q, %q: %s, %q: %s}",
			"type", "OCIRepository",
			"baseUrl", strconv.Quote(r.BaseUrl),
			"subPath", strconv.Quote(r.SubPath)), nil
	case *ctfv1.Repository:
		return fmt.Sprintf("{%q: %q, %q: %s}",
			"type", "CommonTransportFormat",
			"filePath", strconv.Quote(r.FilePath)), nil
	default:
		return "", fmt.Errorf("unsupported target repository type %T", repo)
	}
}

// uploaderAliases returns the aliases uploader expressions of resource i see: `resource`
// as dyn(<its path in the descriptor environment node>), so fields of any access type can
// be tested and read; `target` as a map literal of the transfer target.
func uploaderAliases(env *uploaderEnv, i int, toSpec runtime.Typed) (map[string]string, error) {
	target, err := targetLiteral(toSpec)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		resourceAlias: "dyn(" + resourceNodePath(env.baseID, i) + ")",
		targetAlias:   target,
	}, nil
}

// matches evaluates the match expression expr with aliases rewritten. An empty
// expression matches; only programmatic configs that bypass Validate reach it. An
// expression that does not compile, does not evaluate, or does not return a bool is an
// error.
func matches(expr string, aliases map[string]string, env *uploaderEnv) (bool, error) {
	if expr == "" {
		return true, nil
	}
	prg, err := env.program(rewriteExpression(expr, aliases))
	if err != nil {
		return false, fmt.Errorf("invalid match %q: %w", expr, err)
	}
	out, _, err := prg.Eval(map[string]any{})
	if err != nil {
		return false, fmt.Errorf("match %q does not evaluate: %w", expr, err)
	}
	selected, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("match %q must evaluate to a bool, got %T", expr, out.Value())
	}
	return selected, nil
}
