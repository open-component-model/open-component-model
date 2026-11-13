package runtime_test

import (
	"encoding/json/v2"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// TestJSONOptions pins the encoding/json/v2 defaults OCM's wire format depends on. Each
// case produces different bytes if its option is dropped, and different bytes mean a
// different descriptor digest, so a signed component version stops verifying.
func TestJSONOptions(t *testing.T) {
	type nilContainers struct {
		Map   map[string]string `json:"map"`
		Slice []string          `json:"slice"`
	}

	tests := []struct {
		name string
		in   any
		want string
	}{
		{
			name: "Deterministic: map members are written in sorted order",
			in:   map[string]string{"c": "3", "a": "1", "b": "2"},
			want: `{"a":"1","b":"2","c":"3"}`,
		},
		{
			name: "FormatNilMapAsNull / FormatNilSliceAsNull: nil stays null",
			in:   nilContainers{},
			want: `{"map":null,"slice":null}`,
		},
		{
			// Normalisation signs these bytes and never escaped; only ConvertToV2Provider
			// opts back into v1's escaping, and its own test covers that.
			name: "HTML characters stay literal, as normalisation always wrote them",
			in:   map[string]string{"k": `a<b>c&d`},
			want: `{"k":"a<b>c&d"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			data, err := json.Marshal(tt.in, runtime.JSONOptions)
			r.NoError(err)
			r.Equal(tt.want, string(data))
		})
	}
}

// TestScheme_Convert_FromRaw_NonCanonicalCasing covers the leniency nested typed data is
// decoded with. A .ocmconfig is read as YAML through encoding/json v1, so a non-canonical
// member spelling survives into the nested Raw bytes; decoding those case-sensitively
// would leave the field empty and report no error, which surfaces far from its cause.
func TestScheme_Convert_FromRaw_NonCanonicalCasing(t *testing.T) {
	r := require.New(t)

	scheme := runtime.NewScheme()
	scheme.MustRegister(&TestType{}, "v1")

	raw := &runtime.Raw{
		Type: runtime.NewVersionedType("TestType", "v1"),
		Data: []byte(`{"type":"TestType/v1","Foo":"bar"}`),
	}

	into := &TestType{}
	r.NoError(scheme.Convert(raw, into))
	r.Equal("bar", into.Foo)
}

// marshalFuncs are the encoding/json/v2 entrypoints whose output depends on [JSONOptions].
var marshalFuncs = map[string]struct{}{
	"Marshal":       {},
	"MarshalWrite":  {},
	"MarshalEncode": {},
}

// optionsExceptions are the call sites that deliberately marshal without [JSONOptions],
// keyed by "<slash-separated path>:<function>". Adding an entry needs a reason in the code.
var optionsExceptions = map[string]string{
	"runtime/unstructured.go:MarshalEncode": "MarshalJSONTo writes into the caller's encoder and inherits the options it was created with",
}

// TestMarshalCallsPassJSONOptions walks the module and asserts every v2 marshal call
// passes [JSONOptions].
//
// The lint rule in golangci.yml bans the encoding/json v1 entrypoints, but it cannot
// inspect arguments: a v2 json.Marshal(v) without the options compiles, lints clean and
// silently restores random map ordering, {} for a nil map and unescaped HTML characters.
// No golden-bytes test would catch it either, because the descriptors in testdata were
// produced by v1.
func TestMarshalCallsPassJSONOptions(t *testing.T) {
	r := require.New(t)

	root, err := filepath.Abs("..")
	r.NoError(err)

	// Walking an fs.FS keeps the paths slash-separated on every OS, so the exception
	// keys below match on Windows too.
	mod := os.DirFS(root)
	fset := token.NewFileSet()
	var offenders []string

	r.NoError(fs.WalkDir(mod, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if base := d.Name(); base == "testdata" || base == "tmp" || strings.HasPrefix(base, ".") && base != "." {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}

		src, err := fs.ReadFile(mod, name)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			return err
		}

		// Only files importing encoding/json/v2 under the name the calls use can offend.
		alias := jsonV2Alias(file)
		if alias == "" {
			return nil
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != alias {
				return true
			}
			if _, ok := marshalFuncs[sel.Sel.Name]; !ok {
				return true
			}
			if _, ok := optionsExceptions[name+":"+sel.Sel.Name]; ok {
				return true
			}
			if !passesJSONOptions(call) {
				offenders = append(offenders, fset.Position(call.Pos()).String())
			}
			return true
		})
		return nil
	}))

	r.Empty(offenders, "every encoding/json/v2 marshal call must pass runtime.JSONOptions, "+
		"otherwise map ordering, nil map/slice rendering and HTML escaping silently change")
}

// jsonV2Alias returns the name encoding/json/v2 is imported under, or "" if it is not.
func jsonV2Alias(file *ast.File) string {
	for _, imp := range file.Imports {
		if imp.Path.Value != `"encoding/json/v2"` {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return "json"
	}
	return ""
}

// passesJSONOptions reports whether any argument of the call mentions JSONOptions.
func passesJSONOptions(call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		found := false
		ast.Inspect(arg, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok && ident.Name == "JSONOptions" {
				found = true
			}
			return !found
		})
		if found {
			return true
		}
	}
	return false
}
