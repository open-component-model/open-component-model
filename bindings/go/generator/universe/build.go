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

const (
	// LoadTargetTypeModule indicates a filesystem directory target
	LoadTargetTypeModule = "module"
	// LoadTargetTypeImport indicates an import path target
	LoadTargetTypeImport = "import"
)

// LoadTarget represents something that should be loaded into the universe
type LoadTarget struct {
	Type     string   // LoadTargetTypeModule or LoadTargetTypeImport
	Path     string   // module directory path or import path
	Patterns []string // specific package patterns within a module (nil = "./...")
	Required bool     // whether failure should stop the build
}

// PackageLoader handles loading packages from various sources with consistent configuration
type PackageLoader struct {
	ctx context.Context
}

// NewPackageLoader creates a new PackageLoader
func NewPackageLoader(ctx context.Context) *PackageLoader {
	return &PackageLoader{ctx: ctx}
}

// LoadTargets loads packages from multiple targets and returns all successfully loaded packages
func (pl *PackageLoader) LoadTargets(targets []LoadTarget) ([]*packages.Package, error) {
	var allPkgs []*packages.Package
	g, ctx := errgroup.WithContext(pl.ctx)
	var mu sync.Mutex

	for _, target := range targets {
		g.Go(func() error {
			pkgs, err := pl.loadTarget(ctx, target)
			if err != nil {
				if target.Required {
					return fmt.Errorf("failed to load required target %s: %w", target.Path, err)
				}
				slog.WarnContext(ctx, "failed to load optional target", "path", target.Path, "error", err)
				return nil
			}

			mu.Lock()
			allPkgs = append(allPkgs, pkgs...)
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return allPkgs, nil
}

// loadTarget loads packages from a single target
func (pl *PackageLoader) loadTarget(ctx context.Context, target LoadTarget) ([]*packages.Package, error) {
	cfg := &packages.Config{
		Context: ctx,
		Tests:   false,
		Mode: packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedFiles |
			packages.NeedImports,
	}

	var pkgs []*packages.Package
	var err error

	switch target.Type {
	case LoadTargetTypeModule:
		cfg.Dir = target.Path
		patterns := target.Patterns
		if len(patterns) == 0 {
			patterns = []string{"./..."}
		}
		pkgs, err = packages.Load(cfg, patterns...)
	case LoadTargetTypeImport:
		pkgs, err = packages.Load(cfg, target.Path)
	default:
		return nil, fmt.Errorf("unknown target type: %s", target.Type)
	}

	if err != nil {
		return nil, err
	}

	if packages.PrintErrors(pkgs) > 0 {
		return nil, fmt.Errorf("package load errors in target %s", target.Path)
	}

	slog.InfoContext(ctx, "loaded packages from target", "type", target.Type, "path", target.Path, "count", len(pkgs))
	return pkgs, nil
}

// Build scans all Go modules reachable from roots and builds a Universe.
// it only considers modules whose files have at least the given marker.
// this is mainly to reduce build time.
func Build(ctx context.Context, marker string, roots ...string) (*Universe, error) {
	// Phase 1: Discovery - Find packages with schema markers
	targets, err := discoverLoadTargets(ctx, marker, roots...)
	if err != nil {
		return nil, err
	}

	if len(targets) == 0 {
		slog.InfoContext(ctx, "no modules with schema markers found")
		return New(), nil
	}

	// Phase 2: Loading - Load only the specific annotated packages
	loader := NewPackageLoader(ctx)
	pkgs, err := loader.LoadTargets(targets)
	if err != nil {
		return nil, err
	}

	// Phase 3: Processing - Build universe from loaded packages
	universe := buildUniverse(ctx, pkgs)
	slog.InfoContext(ctx, "universe built", "types", len(universe.Types))
	return universe, nil
}

// discoverLoadTargets finds specific packages with schema markers and prepares targeted load targets.
// Instead of loading entire modules with "./...", it identifies the specific packages containing
// markers and loads only those, dramatically reducing the number of packages type-checked.
func discoverLoadTargets(ctx context.Context, marker string, roots ...string) ([]LoadTarget, error) {
	modRoots, err := findModuleRoots(roots)
	if err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "scanning for schema markers", "modules", len(modRoots))
	pkgsByModule, err := findPackagesWithSchemaMarkers(ctx, marker, modRoots)
	if err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "found packages with schema markers",
		"modules", len(pkgsByModule),
		"total_modules", len(modRoots),
	)

	var targets []LoadTarget
	for modRoot, relPkgs := range pkgsByModule {
		targets = append(targets, LoadTarget{
			Type:     LoadTargetTypeModule,
			Path:     modRoot,
			Patterns: relPkgs,
			Required: true,
		})
	}

	// Always include runtime module for external references
	targets = append(targets, LoadTarget{
		Type:     LoadTargetTypeImport,
		Path:     RuntimePackage,
		Required: false, // Runtime module is optional to not break builds
	})

	return targets, nil
}

// buildUniverse processes loaded packages into a Universe
func buildUniverse(ctx context.Context, pkgs []*packages.Package) *Universe {
	u := New()
	for _, pkg := range pkgs {
		u.recordImports(pkg)
		scanPackage(u, pkg)
	}
	return u
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

// findPackagesWithSchemaMarkers walks module roots using the filesystem to find
// Go files containing the marker, returning a map of module root -> relative package patterns.
// This uses direct filesystem walking instead of packages.Load(NeedFiles) to avoid
// spawning expensive go list subprocesses during discovery.
func findPackagesWithSchemaMarkers(_ context.Context, marker string, modRoots []string) (map[string][]string, error) {
	result := make(map[string][]string)
	var mu sync.Mutex
	var g errgroup.Group

	for _, modRoot := range modRoots {
		g.Go(func() error {
			relPkgs, err := walkModuleForMarkers(modRoot, marker)
			if err != nil {
				return err
			}

			if len(relPkgs) > 0 {
				mu.Lock()
				result[modRoot] = relPkgs
				mu.Unlock()
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return result, nil
}

// walkModuleForMarkers walks a module directory tree and returns relative package
// patterns (e.g. "./spec/v1") for directories containing Go files with the marker.
func walkModuleForMarkers(modRoot, marker string) ([]string, error) {
	seen := map[string]struct{}{}
	var relPkgs []string

	err := filepath.WalkDir(modRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip directories the Go tool ignores: vendor, testdata, hidden, underscore-prefixed
			name := d.Name()
			if name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		name := d.Name()
		// Skip test files and generated files — they don't contain source markers
		if strings.HasSuffix(name, "_test.go") || strings.HasPrefix(name, "zz_generated") {
			return nil
		}

		dir := filepath.Dir(p)
		if _, ok := seen[dir]; ok {
			return nil
		}

		found, err := fileContainsSchemaMarker(p, marker)
		if err != nil || !found {
			return nil //nolint:nilerr // skip unreadable files
		}
		seen[dir] = struct{}{}

		rel, err := filepath.Rel(modRoot, dir)
		if err != nil {
			return err
		}
		relPkgs = append(relPkgs, "./"+filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}

	return relPkgs, nil
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
