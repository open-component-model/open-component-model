package universe_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/generator/universe"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()

	writeFile(t, dir, "go.mod", `module example.com/testmod

go 1.22
`)
	for name, content := range files {
		writeFile(t, dir, name, content)
	}
	return dir
}

func TestDefinition(t *testing.T) {
	key := universe.TypeKey{
		PkgPath:  "example.com/foo/bar",
		TypeName: "MyType",
	}
	require.Equal(
		t,
		"example.com.foo.bar.MyType",
		universe.Definition(key),
	)
}

func TestConstLiteral(t *testing.T) {
	src := `
package p

type T string

const (
	A T = "hello"
	B T = "world"
)
`
	dir := writeModule(t, map[string]string{
		"p.go": src,
	})

	u, err := universe.Build(t.Context(), []string{dir})
	require.NoError(t, err)

	ti := u.LookupType("example.com/testmod", "T")
	require.NotNil(t, ti)
	require.Len(t, ti.Consts, 2)

	val, ok := ti.Consts[0].Literal()
	require.True(t, ok)
	require.Equal(t, "hello", val)
}

func TestLookupType(t *testing.T) {
	src := `
package p

type A struct{}
type B struct{}
`
	dir := writeModule(t, map[string]string{
		"p.go": src,
	})

	u, err := universe.Build(t.Context(), []string{dir})
	require.NoError(t, err)

	require.NotNil(t, u.LookupType("example.com/testmod", "A"))
	require.NotNil(t, u.LookupType("example.com/testmod", "B"))
	require.Nil(t, u.LookupType("example.com/testmod", "C"))
}

func TestResolveExpr_Ident(t *testing.T) {
	src := `
package p

type A struct{}
type B struct {
	F A
}
`
	dir := writeModule(t, map[string]string{
		"p.go": src,
	})

	u, err := universe.Build(t.Context(), []string{dir})
	require.NoError(t, err)

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	require.NoError(t, err)

	var fieldType ast.Expr
	for _, decl := range f.Decls {
		if gd, ok := decl.(*ast.GenDecl); ok {
			for _, spec := range gd.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.Name == "B" {
					st := ts.Type.(*ast.StructType)
					fieldType = st.Fields.List[0].Type
				}
			}
		}
	}
	require.NotNil(t, fieldType)

	pkg := u.LookupType("example.com/testmod", "B").Pkg
	ti, ok := u.ResolveExpr(pkg.TypesInfo, pkg.Types.Path(), fieldType)
	require.True(t, ok)
	require.Equal(t, "A", ti.Key.TypeName)
}

func TestResolveExpr_Selector(t *testing.T) {
	src := `
package p

import "time"

type A struct {
	T time.Duration
}
`
	dir := writeModule(t, map[string]string{
		"p.go": src,
	})

	u, err := universe.Build(t.Context(), []string{dir})
	require.NoError(t, err)

	// time.Duration should not be resolved (external type)
	pkg := u.LookupType("example.com/testmod", "A").Pkg

	var sel ast.Expr
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			if gd, ok := decl.(*ast.GenDecl); ok {
				for _, spec := range gd.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.Name == "A" {
						st := ts.Type.(*ast.StructType)
						sel = st.Fields.List[0].Type
					}
				}
			}
		}
	}

	ti, ok := u.ResolveExpr(pkg.TypesInfo, pkg.Types.Path(), sel)
	require.False(t, ok)
	require.Nil(t, ti)
}

func TestFindModuleRoots_NoModule(t *testing.T) {
	_, err := universe.Build(t.Context(), []string{t.TempDir()})
	require.Error(t, err)
}
