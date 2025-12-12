package universe

import (
	"fmt"
	"go/ast"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"
)

func (u *Universe) RegisterTypes(path string, file *ast.File) {
	dir := filepath.Dir(path)

	pkgPath, err := u.GuessPackagePath(dir)
	if err != nil {
		// unresolved package; skip type registration
		return
	}

	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			u.AddType(pkgPath, ts.Name.Name, path, file, ts, gd)
		}
	}
}

func (u *Universe) GuessPackagePath(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	// Fast-cache hit
	if p := u.pkgDirImportPath[abs]; p != "" {
		return p, nil
	}

	// Need to load module for this directory
	modRoot, err := findModuleRoot(abs)
	if err != nil {
		return "", err
	}

	// Load this module only once
	if !u.moduleCache[modRoot] {
		if err := u.loadModule(modRoot); err != nil {
			return "", err
		}
		u.moduleCache[modRoot] = true
	}

	// Now it must be populated
	if p := u.pkgDirImportPath[abs]; p != "" {
		return p, nil
	}

	return "", fmt.Errorf("no package path found for %s", abs)
}

func findModuleRoot(dir string) (string, error) {
	cur := dir
	for {
		modFile := filepath.Join(cur, "go.mod")

		if data, err := os.ReadFile(modFile); err == nil {
			mf, err := modfile.Parse(modFile, data, nil)
			if err != nil {
				return "", fmt.Errorf("parsing %s: %w", modFile, err)
			}
			if mf.Module == nil || mf.Module.Mod.Path == "" {
				return "", fmt.Errorf("module path missing in %s", modFile)
			}
			return cur, nil
		}

		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return "", fmt.Errorf("go.mod not found above %s", dir)
}

func (u *Universe) loadModule(modRoot string) error {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedModule,
		Dir:  modRoot,
	}

	// Load entire module with pattern "./..."
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return fmt.Errorf("loading module at %s failed: %w", modRoot, err)
	}
	if packages.PrintErrors(pkgs) > 0 {
		return fmt.Errorf("loading module at %s produced errors", modRoot)
	}

	// Populate directory → import path cache
	count := 0
	for _, p := range pkgs {
		for _, f := range p.GoFiles {
			abs := filepath.Dir(f)
			u.pkgDirImportPath[abs] = p.PkgPath
			count++
		}
	}

	slog.Debug("module directories indexed", "modRoot", modRoot, "count", count)
	return nil
}
