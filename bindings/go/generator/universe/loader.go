package universe

import (
	"fmt"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Build loads all Go files from the given root directories and
// populates the Universe with import maps and discovered structs.
func Build(roots []string) (*Universe, error) {
	u := New()
	fset := token.NewFileSet()

	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil { // error from Walk
				return err
			}
			if info.IsDir() {
				return nil
			}

			if !isEligibleGoFile(path) {
				return nil
			}

			file, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if parseErr != nil {
				return parseErr
			}

			u.RecordImports(path, file)
			u.RegisterTypes(path, file)
			u.RegisterConstsFromFile(path, file)

			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	// Always try to load the OCM Runtime package to have core types available.
	if err := u.LoadFromGoImport(RuntimePackage); err != nil {
		slog.Warn("Cannot load OCM runtime package", "package", RuntimePackage, "error", err)
	}

	return u, nil
}

func isEligibleGoFile(path string) bool {
	if !strings.HasSuffix(path, ".go") {
		return false
	}

	base := filepath.Base(path)

	// ignore generated files
	if strings.HasPrefix(base, "zz_generated") {
		return false
	}

	return true
}

func (u *Universe) LoadFromGoImport(importPath string) error {
	cfg := &packages.Config{
		Mode: packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedFiles,
	}

	pkgs, err := packages.Load(cfg, importPath)
	if err != nil {
		return err
	}
	if len(pkgs) == 0 {
		return fmt.Errorf("no package found for %s", importPath)
	}

	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			// pkg.Fset is always available for packages.Load
			pos := pkg.Fset.Position(file.Pos())
			filePath := pos.Filename
			u.RecordImports(filePath, file)
			u.RegisterTypes(filePath, file)
		}
	}

	return nil
}
