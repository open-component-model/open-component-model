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
	Type     string // LoadTargetTypeModule or LoadTargetTypeImport
	Path     string // module directory path or import path
	Required bool   // whether failure should stop the build
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
		pkgs, err = packages.Load(cfg, "./...")
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
	// Phase 1: Discovery - Find modules with schema markers
	targets, err := discoverLoadTargets(ctx, marker, roots...)
	if err != nil {
		return nil, err
	}

	if len(targets) == 0 {
		slog.InfoContext(ctx, "no modules with schema markers found")
		return New(), nil
	}

	// Phase 2: Loading - Load all packages from discovered targets
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

// discoverLoadTargets finds all modules with schema markers and prepares load targets
func discoverLoadTargets(ctx context.Context, marker string, roots ...string) ([]LoadTarget, error) {
	modRoots, err := findModuleRoots(roots)
	if err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "scanning for schema markers", "modules", len(modRoots))
	relevantModules, err := findModulesWithSchemaMarkers(ctx, marker, modRoots)
	if err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "found modules with schema markers", "modules", len(relevantModules), "total", len(modRoots))

	// Prepare load targets for schema modules
	var targets []LoadTarget
	for _, module := range relevantModules {
		targets = append(targets, LoadTarget{
			Type:     LoadTargetTypeModule,
			Path:     module,
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

// findModulesWithSchemaMarkers scans modules for schema markers and returns only relevant modules
func findModulesWithSchemaMarkers(ctx context.Context, marker string, modRoots []string) ([]string, error) {
	var relevantModules []string
	var mu sync.Mutex
	g, ctx := errgroup.WithContext(ctx)

	for _, modRoot := range modRoots {
		g.Go(func() error {
			hasMarker, err := moduleHasSchemaMarkers(ctx, modRoot, marker)
			if err != nil {
				return err
			}

			if hasMarker {
				mu.Lock()
				relevantModules = append(relevantModules, modRoot)
				mu.Unlock()
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return relevantModules, nil
}

// moduleHasSchemaMarkers checks if a module contains any files with schema markers
func moduleHasSchemaMarkers(ctx context.Context, modRoot, marker string) (bool, error) {
	// Use go list to discover all packages (much faster than file walking)
	cfg := &packages.Config{
		Context: ctx,
		Dir:     modRoot,
		Tests:   false,
		Mode:    packages.NeedFiles, // Only need file paths
	}

	allPkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return false, err
	}

	for _, pkg := range allPkgs {
		// Check if any Go files in this package contain schema markers
		for _, goFile := range pkg.GoFiles {
			found, err := fileContainsSchemaMarker(goFile, marker)
			if err != nil {
				continue // skip files we can't read
			}
			if found {
				return true, nil
			}
		}
	}

	return false, nil
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
