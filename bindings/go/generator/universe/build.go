package universe

import (
	"bufio"
	"context"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"io/fs"
	"log/slog"
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
// it only considers modules whose files have at least the given marker.
// this is mainly to reduce build time.
func Build(ctx context.Context, marker string, roots ...string) (*Universe, error) {
	modRoots, err := findModuleRoots(roots)
	if err != nil {
		return nil, err
	}

	// Phase 1: Quick scan for packages with schema markers
	slog.InfoContext(ctx, "scanning for schema markers", "modules", len(modRoots))
	relevantPackages, err := findPackagesWithSchemaMarkers(ctx, marker, modRoots...)
	if err != nil {
		return nil, err
	}

	if len(relevantPackages) == 0 {
		slog.InfoContext(ctx, "no packages with schema markers found")
		return New(), nil
	}

	slog.InfoContext(ctx, "found packages with schema markers", "packages", len(relevantPackages))

	// Phase 2: Load only relevant packages + discover their dependencies
	u := New()

	// Discover dependencies by loading packages with imports enabled
	slog.InfoContext(ctx, "discovering dependencies", "packages", len(relevantPackages))
	allDependencies, err := discoverPackageDependencies(ctx, relevantPackages)
	if err != nil {
		return nil, err
	}

	// Always include runtime package for external references
	allPackagesToLoad := append(allDependencies, RuntimePackage)

	// Remove duplicates
	seen := make(map[string]struct{})
	var uniquePackages []string
	for _, pkg := range allPackagesToLoad {
		if _, exists := seen[pkg]; !exists && pkg != "" {
			seen[pkg] = struct{}{}
			uniquePackages = append(uniquePackages, pkg)
		}
	}

	slog.InfoContext(ctx, "loading packages with dependencies", "total", len(uniquePackages))

	pkgs, err := loadSpecificPackages(ctx, uniquePackages)
	if err != nil {
		return nil, err
	}

	for _, pkg := range pkgs {
		u.recordImports(pkg)
		scanPackage(u, pkg)
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

// findPackagesWithSchemaMarkers quickly scans Go source files for schema markers
// without doing full package loading. only packages with a file with the given marker are considered
func findPackagesWithSchemaMarkers(ctx context.Context, marker string, modRoots ...string) ([]string, error) {
	var relevantPackages []string
	var mu sync.Mutex
	g, ctx := errgroup.WithContext(ctx)

	for _, modRoot := range modRoots {
		g.Go(func() error {
			foundPackages, err := scanModuleForSchemaMarkers(ctx, modRoot, marker)
			if err != nil {
				return err
			}

			if len(foundPackages) > 0 {
				mu.Lock()
				relevantPackages = append(relevantPackages, foundPackages...)
				mu.Unlock()
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return relevantPackages, nil
}

// scanModuleForSchemaMarkers scans a single module for packages containing schema markers.
// note that only
func scanModuleForSchemaMarkers(ctx context.Context, modRoot, marker string) ([]string, error) {
	// First, use go list to discover all packages (much faster than file walking)
	cfg := &packages.Config{
		Context: ctx,
		Dir:     modRoot,
		Tests:   false,
		Mode:    packages.NeedName | packages.NeedFiles, // Need name to get proper import paths
	}

	allPkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, err
	}

	var relevantPkgs []string
	for _, pkg := range allPkgs {
		// Check if any Go files in this package contain schema markers
		hasMarker := false
		for _, goFile := range pkg.GoFiles {
			found, err := fileContainsSchemaMarker(goFile, marker)
			if err != nil {
				continue // skip files we can't read
			}
			if found {
				hasMarker = true
				break
			}
		}

		if hasMarker {
			slog.InfoContext(ctx, "found package with schema marker", "package", pkg.PkgPath)
			relevantPkgs = append(relevantPkgs, pkg.PkgPath)
		}
	}

	return relevantPkgs, nil
}

// discoverPackageDependencies loads packages with imports to discover their dependencies
func discoverPackageDependencies(ctx context.Context, packagePaths []string) ([]string, error) {
	if len(packagePaths) == 0 {
		return packagePaths, nil
	}

	// Load packages with minimal info to get import dependencies
	cfg := &packages.Config{
		Context: ctx,
		Tests:   false,
		Mode:    packages.NeedName | packages.NeedImports,
	}

	pkgs, err := packages.Load(cfg, packagePaths...)
	if err != nil {
		return packagePaths, nil // Return original packages if dependency discovery fails
	}

	// Collect all dependencies
	depMap := make(map[string]struct{})
	for _, pkg := range pkgs {
		// Add the original package
		if pkg.PkgPath != "" {
			depMap[pkg.PkgPath] = struct{}{}
		}

		// Add its dependencies (but only those within our project)
		for _, dep := range pkg.Imports {
			if dep.PkgPath != "" && strings.Contains(dep.PkgPath, "ocm.software/open-component-model") {
				depMap[dep.PkgPath] = struct{}{}
			}
		}
	}

	// Convert map to slice
	var allDeps []string
	for dep := range depMap {
		allDeps = append(allDeps, dep)
	}

	return allDeps, nil
}

// fileContainsSchemaMarker quickly checks if a Go file contains the schema marker
func fileContainsSchemaMarker(filePath, marker string) (bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		// Look for comment lines containing the schema marker
		if strings.Contains(line, marker) {
			return true, nil
		}
	}

	return false, scanner.Err()
}

// loadSpecificPackages loads only the specified packages by their import paths
func loadSpecificPackages(ctx context.Context, packagePaths []string) ([]*packages.Package, error) {
	if len(packagePaths) == 0 {
		return nil, nil
	}

	cfg := &packages.Config{
		Context: ctx,
		Tests:   false,
		Mode: packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedFiles |
			packages.NeedImports,
	}

	pkgs, err := packages.Load(cfg, packagePaths...)
	if err != nil {
		return nil, err
	}

	if packages.PrintErrors(pkgs) > 0 {
		return nil, fmt.Errorf("package load errors for selected packages: %s", strings.Join(packagePaths, ", "))
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
