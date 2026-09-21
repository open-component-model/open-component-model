package input

import (
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/ext"

	celparser "ocm.software/open-component-model/bindings/go/cel/expression/parser"
	"ocm.software/open-component-model/bindings/go/wget/checksum"
	v1 "ocm.software/open-component-model/bindings/go/wget/spec/input/v1"
)

// checksumURLEnv is the CEL environment used to evaluate a ChecksumSource.URL
// expression. It exposes the resource under construction plus the current
// algorithm as dedicated variables:
//
//   - resource: the wget input under construction with parsed url subfields
//     (url.path/host/scheme).
//   - ext: the algorithm's file extension (e.g. "sha256").
//   - alg: the algorithm's OCM name (e.g. "SHA-256").
//
// The environment is built once because CEL compilation is not free and the
// variable declarations never change per-input.
var checksumURLEnv = sync.OnceValues[*cel.Env, error](func() (*cel.Env, error) {
	return cel.NewEnv(
		ext.Strings(),
		cel.Variable("resource", cel.DynType),
		cel.Variable("ext", cel.StringType),
		cel.Variable("alg", cel.StringType),
	)
})

// checksumURLResolver compiles the ChecksumSource.URL expression once and returns
// a resolver closure that renders it against the wget input for a given algorithm.
// An empty expr returns nil so [checksum.Resolve] falls back to the default
// `<baseURL>.<alg.Extension>` (Maven's convention).
func checksumURLResolver(expr string, wget *v1.Wget) (func(baseURL string, alg checksum.Algorithm) (string, error), error) {
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" {
		return nil, nil
	}
	standalone, err := celparser.IsStandaloneExpression(trimmed)
	if err != nil {
		return nil, fmt.Errorf("invalid checksum source url CEL expression %q: %w", expr, err)
	}
	if !standalone {
		return nil, fmt.Errorf("checksum source url must be a single CEL expression wrapped in ${...}, got %q", expr)
	}
	// Strip the ${…} wrapper — celparser guarantees the shape.
	inner := trimmed[len("${") : len(trimmed)-len("}")]

	env, err := checksumURLEnv()
	if err != nil {
		return nil, fmt.Errorf("cannot build checksum url CEL environment: %w", err)
	}
	ast, issues := env.Compile(inner)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("cannot compile checksum source url %q: %w", expr, issues.Err())
	}
	if out := ast.OutputType(); out != cel.StringType && out != cel.DynType {
		return nil, fmt.Errorf("checksum source url %q must evaluate to a string, got %s", expr, out)
	}
	prog, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("cannot build checksum source url program: %w", err)
	}

	resource := resourceNode(wget)

	return func(_ string, alg checksum.Algorithm) (string, error) {
		out, _, err := prog.Eval(map[string]any{
			"resource": resource,
			"ext":      alg.Extension,
			"alg":      alg.OCMName,
		})
		if err != nil {
			return "", fmt.Errorf("checksum source url evaluation failed: %w", err)
		}
		s, ok := celToString(out)
		if !ok {
			return "", fmt.Errorf("checksum source url did not evaluate to a string, got %T", out.Value())
		}
		if _, perr := url.Parse(s); perr != nil {
			return "", fmt.Errorf("checksum source url resolved to an invalid url %q: %w", s, perr)
		}
		return s, nil
	}, nil
}

// resourceNode exposes the wget input as a CEL-addressable map with a parsed
// `url` sub-object (path/host/scheme/raw) so an expression can compose the
// checksum URL from the artifact's URL parts without a URL parser in CEL.
func resourceNode(wget *v1.Wget) map[string]any {
	urlNode := map[string]any{
		"raw":    wget.URL,
		"path":   "",
		"host":   "",
		"scheme": "",
	}
	if parsed, err := url.Parse(wget.URL); err == nil {
		urlNode["path"] = parsed.Path
		urlNode["host"] = parsed.Host
		urlNode["scheme"] = parsed.Scheme
	}
	return map[string]any{
		"url":        urlNode,
		"mediaType":  wget.MediaType,
		"verb":       wget.Verb,
		"header":     wget.Header,
		"noRedirect": wget.NoRedirect,
	}
}

// celToString extracts a Go string from a CEL value that evaluates to a string.
// It accepts a native ref.Val string as well as the dyn(string) case.
func celToString(v ref.Val) (string, bool) {
	if v == nil {
		return "", false
	}
	if v.Type() == types.StringType {
		if s, ok := v.Value().(string); ok {
			return s, true
		}
	}
	if s, ok := v.Value().(string); ok {
		return s, true
	}
	return "", false
}
