package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsValidGoFile(t *testing.T) {
	assert.True(t, isValidGoFile("example.go"))
	assert.False(t, isValidGoFile("example_test.go"))
	assert.False(t, isValidGoFile("zz_generated.somefile.go"))
	assert.False(t, isValidGoFile("example.txt"))
}

func TestHasMarker(t *testing.T) {
	// This will be tested via actual file in testdata
	fset := token.NewFileSet()
	path := filepath.Join("test", "type.go")
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	require.NoError(t, err)

	decl := file.Decls[1].(*ast.GenDecl)
	assert.True(t, hasMarker(decl.Doc))
}

func TestHasRuntimeTypeField(t *testing.T) {
	fset := token.NewFileSet()
	path := filepath.Join("test", "type.go")
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	require.NoError(t, err)

	decl := file.Decls[1].(*ast.GenDecl)
	spec := decl.Specs[0].(*ast.TypeSpec)
	structType := spec.Type.(*ast.StructType)

	assert.True(t, hasRuntimeTypeField(structType))
}

func TestReadModulePath(t *testing.T) {
	path := filepath.Join("..", "go.mod")
	modulePath, err := readModulePath(path)
	require.NoError(t, err)
	assert.Equal(t, "ocm.software/open-component-model/bindings/go/generator", modulePath)
}

func TestGetImportPath(t *testing.T) {
	path := filepath.Join("test")
	importPath, err := getImportPath(path)
	require.NoError(t, err)
	assert.Equal(t, "ocm.software/open-component-model/bindings/go/generator/ocmtypegen/test", importPath)
}

func TestFindGoPackages(t *testing.T) {
	packages, err := findGoPackages("test")
	require.NoError(t, err)
	assert.Contains(t, packages, filepath.Join("test"))
}

func TestScanSinglePackage(t *testing.T) {
	pkgName, types, err := scanSinglePackage(filepath.Join("test"))
	require.NoError(t, err)
	assert.Equal(t, "test", pkgName)
	assert.Contains(t, types, "SampleType")
}
