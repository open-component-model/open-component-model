package universe

import (
	"go/ast"
	"go/token"
	"log/slog"
	"strconv"
	"strings"
)

// New creates an empty Universe.
func New() *Universe {
	return &Universe{
		Types:            map[TypeKey]*TypeInfo{},
		ImportMaps:       map[string]map[string]string{},
		pkgDirImportPath: map[string]string{},
		moduleCache:      map[string]bool{},
	}
}

// Universe represents all Go types and imports discovered during scanning.
//
//   - Types maps each discovered struct type to its TypeInfo.
//   - ImportMaps tracks, per file, the import alias → full import path mapping.
//     This is used to resolve selector expressions (e.g. external.Type).
//
// The Universe is immutable after Build() and is consumed by the Generator.
type Universe struct {
	Types            map[TypeKey]*TypeInfo        // all named struct types in all scanned packages
	ImportMaps       map[string]map[string]string // filePath → (alias → full package path)
	pkgDirImportPath map[string]string            // cache: package dir → resolved import path
	moduleCache      map[string]bool              // cache: module path → already observed and loaded
}

///////////////////////////////////////////////////////////////////////////////
// Type Keys and Type Information
///////////////////////////////////////////////////////////////////////////////

// TypeKey uniquely identifies a Go type within the Universe.
//
// PkgPath is the resolved Go module import path of the package containing
// the type (e.g. "ocm.software/open-component-model/bindings/go/runtime").
//
// TypeName is the name of the exported type (e.g. "Raw", "Type").
type TypeKey struct {
	PkgPath  string
	TypeName string
}

// TypeInfo stores all structural information needed for schema generation.
//
// Key is the unique (PkgPath, TypeName) key identifying the type.
// Struct is the underlying *ast.StructType of the named struct.
// File is the parsed *ast.File containing the type definition.
// FilePath is the absolute path to the Go source file declaring the type.
// TypeSpec is the *ast.TypeSpec for the type.
// GenDecl is the surrounding *ast.GenDecl, used for comment extraction.
//
// TypeInfo does NOT store whether the type should be emitted — that is
// determined by the generator (root type) or by reference tracking.
//
// Extended: Consts holds constants explicitly declared with this type,
// detected via AST-based analysis in RegisterConstsFromFile.
type TypeInfo struct {
	Key      TypeKey
	Expr     ast.Expr
	Struct   *ast.StructType
	File     *ast.File
	FilePath string
	TypeSpec *ast.TypeSpec
	GenDecl  *ast.GenDecl
	Consts   []*ConstInfo
}

// ConstInfo stores AST information about a single constant belonging to a type.
//
// The Value field may be nil if the constant declaration does not include
// an explicit RHS for this name (e.g., `const ( A = "x"; B )`).
type ConstInfo struct {
	Name    string   // identifier name (e.g. "SignatureEncodingPolicyPlain")
	Value   ast.Expr // literal/expression assigned (may be nil)
	Doc     *ast.CommentGroup
	Comment *ast.CommentGroup
}

func (c *ConstInfo) Literal() (string, bool) {
	bl, ok := c.Value.(*ast.BasicLit)
	if !ok {
		return "", false
	}
	if bl.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(bl.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

// RecordImports collects the import alias mapping for a file.
//
// Example:
//
//	import foo "example.com/x"
//	import "example.com/y/z"
//
// yields:
//
//	aliasMap["foo"] = "example.com/x"
//	aliasMap["z"]   = "example.com/y/z"
//
// This map is used to resolve selector expressions:
//
//	foo.Type  → package "example.com/x"
//	z.Other   → package "example.com/y/z"
//
// The Universe must record imports *before* type resolution.
func (u *Universe) RecordImports(filePath string, f *ast.File) {
	aliasMap := map[string]string{}

	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		alias := ""

		if imp.Name != nil {
			alias = imp.Name.Name // explicit alias
		} else {
			parts := strings.Split(path, "/")
			alias = parts[len(parts)-1] // use last path segment
		}

		aliasMap[alias] = path
	}

	u.ImportMaps[filePath] = aliasMap
}

// AddType registers a single named struct type discovered in a file.
//
// pkgPath  - resolved import path for the package where the type lives
// typeName - name of the Go type (e.g. "LocalBlob")
// filePath - absolute path of the file containing the type
// st       - the underlying *ast.StructType node
// file     - parsed *ast.File for additional context (imports, comments)
// typeSpec - original *ast.TypeSpec for name, comments, and tags
// gd       - *ast.GenDecl containing the TypeSpec
//
// The generator uses this information to:
//
//   - build root schemas
//   - inspect fields and nested structs
//   - resolve references between packages
func (u *Universe) AddType(
	pkgPath, typeName, filePath string,
	file *ast.File,
	ts *ast.TypeSpec,
	gd *ast.GenDecl,
) {
	key := TypeKey{PkgPath: pkgPath, TypeName: typeName}

	if _, exists := u.Types[key]; exists {
		slog.Debug("type already registered, skipping", "pkg", pkgPath, "type", typeName)
		return
	}

	st, _ := ts.Type.(*ast.StructType)
	u.Types[key] = &TypeInfo{
		Key:      key,
		FilePath: filePath,
		File:     file,
		TypeSpec: ts,
		GenDecl:  gd,
		Expr:     ts.Type,
		Struct:   st, // nil if not a struct
	}
}
