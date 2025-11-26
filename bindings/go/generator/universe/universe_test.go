package universe

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecordImportsAddTypeAndLookupType(t *testing.T) {
	u := New()

	filePath := filepath.Join(t.TempDir(), "file.go")
	file := &ast.File{
		Imports: []*ast.ImportSpec{
			{Path: &ast.BasicLit{Value: "\"example.com/foo\""}, Name: ast.NewIdent("foo")},
			{Path: &ast.BasicLit{Value: "\"example.com/bar/baz\""}},
		},
	}

	u.RecordImports(filePath, file)

	require.Len(t, u.ImportMaps[filePath], 2)
	require.Equal(t, "example.com/foo", u.ImportMaps[filePath]["foo"])
	require.Equal(t, "example.com/bar/baz", u.ImportMaps[filePath]["baz"])

	structSpec := &ast.TypeSpec{
		Name: ast.NewIdent("Sample"),
		Type: &ast.StructType{},
	}
	nonStructSpec := &ast.TypeSpec{
		Name: ast.NewIdent("Alias"),
		Type: ast.NewIdent("string"),
	}

	u.AddType("example.com/pkg", "Sample", filePath, file, structSpec, &ast.GenDecl{Tok: token.TYPE})
	u.AddType("example.com/pkg", "Alias", filePath, file, nonStructSpec, &ast.GenDecl{Tok: token.TYPE})

	ti := u.LookupType("example.com/pkg", "Sample")
	require.NotNil(t, ti)
	require.NotNil(t, ti.Struct)

	alias := u.LookupType("example.com/pkg", "Alias")
	require.NotNil(t, alias)
	require.Nil(t, alias.Struct)
}

func TestResolveIdent(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "file.go")
	otherPath := filepath.Join(t.TempDir(), "other.go")

	runtimeSpec := &ast.TypeSpec{
		Name: ast.NewIdent("RuntimeType"),
		Type: &ast.Ident{Name: "struct"},
	}
	selectorSpec := &ast.TypeSpec{
		Name: ast.NewIdent("Alias"),
		Type: &ast.SelectorExpr{X: ast.NewIdent("runtime"), Sel: ast.NewIdent("Selected")},
	}
	starAliasSpec := &ast.TypeSpec{
		Name: ast.NewIdent("PointerAlias"),
		Type: &ast.StarExpr{X: &ast.SelectorExpr{X: ast.NewIdent("runtime"), Sel: ast.NewIdent("Starred")}},
	}

	u := &Universe{
		Types: map[TypeKey]*TypeInfo{
			{PkgPath: "example.com/pkg", TypeName: "Local"}: {
				Key: TypeKey{PkgPath: "example.com/pkg", TypeName: "Local"},
			},
			{PkgPath: "example.com/runtime", TypeName: "RuntimeType"}: {
				Key:      TypeKey{PkgPath: "example.com/runtime", TypeName: "RuntimeType"},
				TypeSpec: runtimeSpec,
			},
			{PkgPath: "example.com/runtime", TypeName: "Alias"}: {
				Key:      TypeKey{PkgPath: "example.com/runtime", TypeName: "Alias"},
				TypeSpec: selectorSpec,
			},
			{PkgPath: "example.com/runtime", TypeName: "PointerAlias"}: {
				Key:      TypeKey{PkgPath: "example.com/runtime", TypeName: "PointerAlias"},
				TypeSpec: starAliasSpec,
			},
		},
		ImportMaps: map[string]map[string]string{
			filePath: {
				"rt": "example.com/runtime",
			},
		},
	}

	ti, ok := u.ResolveIdent(filePath, "example.com/pkg", ast.NewIdent("Local"))
	require.True(t, ok)
	require.Equal(t, "Local", ti.Key.TypeName)

	ti, ok = u.ResolveIdent(filePath, "example.com/pkg", ast.NewIdent("RuntimeType"))
	require.True(t, ok)
	require.Equal(t, "RuntimeType", ti.Key.TypeName)

	ti, ok = u.ResolveIdent(filePath, "example.com/pkg", ast.NewIdent("Selected"))
	require.True(t, ok)
	require.Equal(t, "Alias", ti.Key.TypeName)

	ti, ok = u.ResolveIdent(filePath, "example.com/pkg", ast.NewIdent("Starred"))
	require.True(t, ok)
	require.Equal(t, "PointerAlias", ti.Key.TypeName)

	_, ok = u.ResolveIdent(otherPath, "example.com/pkg", ast.NewIdent("Missing"))
	require.False(t, ok)
}

func TestResolveSelector(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "file.go")

	u := &Universe{
		Types: map[TypeKey]*TypeInfo{
			{PkgPath: "example.com/runtime", TypeName: "Type"}: {
				Key: TypeKey{PkgPath: "example.com/runtime", TypeName: "Type"},
			},
		},
		ImportMaps: map[string]map[string]string{
			filePath: {
				"rt": "example.com/runtime",
			},
		},
	}

	ti, ok := u.ResolveSelector(filePath, &ast.SelectorExpr{X: ast.NewIdent("rt"), Sel: ast.NewIdent("Type")})
	require.True(t, ok)
	require.Equal(t, "Type", ti.Key.TypeName)

	_, ok = u.ResolveSelector(filePath, &ast.SelectorExpr{X: ast.NewIdent("missing"), Sel: ast.NewIdent("Type")})
	require.False(t, ok)
}

func TestRegisterTypes(t *testing.T) {
	dir := t.TempDir()

	// Create real go.mod
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module example.com/module\n"),
		0o644,
	))

	// Write a real Go file so packages.Load sees a valid buildable module
	filePath := filepath.Join(dir, "types.go")
	require.NoError(t, os.WriteFile(filePath, []byte(`
package module

type First struct{}
type Second string
`), 0o644))

	// Parse the file into an AST
	fset := token.NewFileSet()
	fileAst, err := parser.ParseFile(fset, filePath, nil, 0)
	require.NoError(t, err)

	u := New()
	u.RegisterTypes(filePath, fileAst)

	require.Contains(t, u.Types, TypeKey{PkgPath: "example.com/module", TypeName: "First"})
	require.Contains(t, u.Types, TypeKey{PkgPath: "example.com/module", TypeName: "Second"})
	require.Len(t, u.Types, 2)
}

func TestIsEligibleGoFile(t *testing.T) {
	testCases := []struct {
		name     string
		path     string
		eligible bool
	}{
		{"go file", "file.go", true},
		{"non go file", "file.txt", false},
		{"generated", "zz_generated.types.go", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.eligible, isEligibleGoFile(tc.path))
		})
	}
}

func TestBuildIntegration(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n"), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(root, "zz_generated.ignore.go"), []byte("package main\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes.txt"), []byte("skip"), 0o644))

	sub := filepath.Join(root, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "go.mod"), []byte("module example.com/app/sub\n"), 0o644))

	mainFile := filepath.Join(root, "main.go")
	require.NoError(t, os.WriteFile(mainFile, []byte("package main\nimport rt \"example.com/app/sub\"\ntype First struct { Ref rt.Type }\n"), 0o644))

	subFile := filepath.Join(sub, "type.go")
	require.NoError(t, os.WriteFile(subFile, []byte("package sub\ntype Type struct {}\n"), 0o644))

	u, err := Build([]string{root})
	require.NoError(t, err)

	_, hasMain := u.Types[TypeKey{PkgPath: "example.com/app", TypeName: "First"}]
	_, hasSub := u.Types[TypeKey{PkgPath: "example.com/app/sub", TypeName: "Type"}]
	require.True(t, hasMain)
	require.True(t, hasSub)

	imports := u.ImportMaps[mainFile]
	require.Equal(t, "example.com/app/sub", imports["rt"])

	_, skipped := u.ImportMaps[filepath.Join(root, "zz_generated.ignore.go")]
	require.False(t, skipped)
}
