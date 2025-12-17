package universe

import (
	"context"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
	"golang.org/x/tools/go/packages"
)

const RuntimePackage = "ocm.software/open-component-model/bindings/go/runtime"

// Universe is an indexed view over Go types discovered during scanning.
// It is immutable after Build.
type Universe struct {
	Types   map[TypeKey]*TypeInfo        // (pkgPath, typeName) -> type
	Imports map[string]map[string]string // pkgPath -> alias -> import path
}

// New creates an empty Universe.
func New() *Universe {
	return &Universe{
		Types:   make(map[TypeKey]*TypeInfo),
		Imports: make(map[string]map[string]string),
	}
}

// TypeKey uniquely identifies a Go type.
type TypeKey struct {
	PkgPath  string
	TypeName string
}

// TypeInfo stores all structural information required by generators.
type TypeInfo struct {
	Key      TypeKey
	Expr     ast.Expr
	Struct   *ast.StructType
	FilePath string
	TypeSpec *ast.TypeSpec
	GenDecl  *ast.GenDecl
	Obj      *types.TypeName
	Consts   []*Const
	Pkg      *packages.Package
}

// Const represents a constant belonging to a named type.
type Const struct {
	Name    string
	Obj     *types.Const
	Doc     *ast.CommentGroup
	Comment *ast.CommentGroup
}

func (c *Const) Literal() (string, bool) {
	if c.Obj == nil {
		return "", false
	}
	v := c.Obj.Val()
	if v.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(v), true
}

//
// ===== Identity helpers =====
//

// Definition returns the canonical $defs identifier for a type.
func Definition(key TypeKey) string {
	return strings.ReplaceAll(key.PkgPath, "/", ".") + "." + key.TypeName
}

func IsRuntimeType(ti *TypeInfo) bool {
	return ti.Key.PkgPath == RuntimePackage && ti.Key.TypeName == "Type"
}

func IsRuntimeRaw(ti *TypeInfo) bool {
	return ti.Key.PkgPath == RuntimePackage && ti.Key.TypeName == "Raw"
}

func IsRuntimeTyped(ti *TypeInfo) bool {
	return ti.Key.PkgPath == RuntimePackage && ti.Key.TypeName == "Typed"
}

// Build scans all Go modules reachable from roots and builds a Universe.
func Build(ctx context.Context, roots []string) (*Universe, error) {
	modRoots, err := findModuleRoots(roots)
	if err != nil {
		return nil, err
	}

	u := New()
	g, ctx := errgroup.WithContext(ctx)
	var mu sync.Mutex

	for _, root := range modRoots {
		root := root
		g.Go(func() error {
			pkgs, err := loadModule(ctx, root)
			if err != nil {
				return err
			}

			mu.Lock()
			defer mu.Unlock()

			for _, pkg := range pkgs {
				u.recordImports(pkg)
				scanPackage(u, pkg)
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}
	return u, nil
}

func findModuleRoots(roots []string) ([]string, error) {
	seen := map[string]struct{}{}
	var modules []string

	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return err
			}

			if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
				abs, err := filepath.Abs(p)
				if err != nil {
					return err
				}
				if _, ok := seen[abs]; !ok {
					seen[abs] = struct{}{}
					modules = append(modules, abs)
				}
				return filepath.SkipDir
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	if len(modules) == 0 {
		return nil, fmt.Errorf("no go.mod found in provided roots")
	}
	return modules, nil
}

func loadModule(ctx context.Context, modRoot string) ([]*packages.Package, error) {
	cfg := &packages.Config{
		Context: ctx,
		Dir:     modRoot,
		Tests:   false,
		Mode: packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedFiles |
			packages.NeedImports,
	}

	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, err
	}
	if packages.PrintErrors(pkgs) > 0 {
		return nil, fmt.Errorf("package load errors in module %s", modRoot)
	}
	return pkgs, nil
}

func scanPackage(u *Universe, pkg *packages.Package) {
	for i, file := range pkg.Syntax {
		filePath := pkg.GoFiles[i]
		scanTypes(u, pkg, filePath, file)
		scanConsts(u, pkg, file)
	}
}

func scanTypes(u *Universe, pkg *packages.Package, filePath string, file *ast.File) {
	pkgPath := pkg.Types.Path()

	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}

		for _, spec := range gd.Specs {
			ts := spec.(*ast.TypeSpec)
			obj, ok := pkg.TypesInfo.Defs[ts.Name].(*types.TypeName)
			if !ok {
				continue
			}

			key := TypeKey{PkgPath: pkgPath, TypeName: obj.Name()}
			if _, exists := u.Types[key]; exists {
				continue
			}

			u.Types[key] = &TypeInfo{
				Key:      key,
				Expr:     ts.Type,
				Struct:   asStruct(ts.Type),
				FilePath: filePath,
				TypeSpec: ts,
				GenDecl:  gd,
				Obj:      obj,
				Pkg:      pkg,
			}
		}
	}
}

func scanConsts(u *Universe, pkg *packages.Package, file *ast.File) {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}

		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			for _, name := range vs.Names {
				obj, ok := pkg.TypesInfo.Defs[name].(*types.Const)
				if !ok {
					continue
				}

				named, ok := obj.Type().(*types.Named)
				if !ok {
					continue
				}

				ti := u.typeByObject(named.Obj())
				if ti == nil {
					continue
				}

				ti.Consts = append(ti.Consts, &Const{
					Name:    obj.Name(),
					Obj:     obj,
					Doc:     vs.Doc,
					Comment: vs.Comment,
				})
			}
		}
	}
}

func asStruct(expr ast.Expr) *ast.StructType {
	st, _ := expr.(*ast.StructType)
	return st
}

func (u *Universe) recordImports(pkg *packages.Package) {
	pkgPath := pkg.Types.Path()
	if _, ok := u.Imports[pkgPath]; ok {
		return
	}

	m := make(map[string]string)
	for _, imp := range pkg.Types.Imports() {
		alias := imp.Name()
		if alias == "" {
			alias = path.Base(imp.Path())
		}
		m[alias] = imp.Path()
	}
	u.Imports[pkgPath] = m
}

func (u *Universe) LookupType(pkgPath, typeName string) *TypeInfo {
	return u.Types[TypeKey{PkgPath: pkgPath, TypeName: typeName}]
}

func (u *Universe) typeByObject(obj *types.TypeName) *TypeInfo {
	if obj == nil || obj.Pkg() == nil {
		return nil
	}
	return u.Types[TypeKey{
		PkgPath:  obj.Pkg().Path(),
		TypeName: obj.Name(),
	}]
}

// ResolveExpr resolves an AST expression to a known TypeInfo using types.Info
// with import-map fallback.
func (u *Universe) ResolveExpr(
	info *types.Info,
	pkgPath string,
	expr ast.Expr,
) (*TypeInfo, bool) {

	switch e := expr.(type) {
	case *ast.Ident:
		return u.resolveIdent(info, pkgPath, e)
	case *ast.SelectorExpr:
		return u.resolveSelector(info, pkgPath, e)
	}
	return nil, false
}

func (u *Universe) resolveIdent(
	info *types.Info,
	pkgPath string,
	id *ast.Ident,
) (*TypeInfo, bool) {

	if obj, ok := info.Uses[id].(*types.TypeName); ok {
		ti := u.typeByObject(obj)
		return ti, ti != nil
	}

	ti := u.Types[TypeKey{PkgPath: pkgPath, TypeName: id.Name}]
	return ti, ti != nil
}

func (u *Universe) resolveSelector(
	info *types.Info,
	pkgPath string,
	sel *ast.SelectorExpr,
) (*TypeInfo, bool) {

	if obj, ok := info.Uses[sel.Sel].(*types.TypeName); ok {
		ti := u.typeByObject(obj)
		return ti, ti != nil
	}

	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return nil, false
	}

	imports := u.Imports[pkgPath]
	if imports == nil {
		return nil, false
	}

	if impPath, ok := imports[pkgIdent.Name]; ok {
		ti := u.Types[TypeKey{PkgPath: impPath, TypeName: sel.Sel.Name}]
		return ti, ti != nil
	}

	return nil, false
}
