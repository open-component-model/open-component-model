package universe

import (
	"bufio"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// Build loads all Go files from the given root directories and
// populates a Universe with import maps and discovered structs.
func Build(roots []string) (*Universe, error) {
	u := New()
	fset := token.NewFileSet()

	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil { // an error from Walk
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

			return nil
		})
		if err != nil {
			return nil, err
		}
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

// GuessPackagePath returns the Go import path of the folder
// containing filePath, based on nearest go.mod.
func GuessPackagePath(folder string) (string, error) {
	abs, _ := filepath.Abs(folder)
	dir := abs

	for {
		goMod := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(goMod); err == nil {
			module, _ := readModulePath(goMod)
			rel, _ := filepath.Rel(dir, abs)
			if rel == "." {
				return module, nil
			}
			return filepath.ToSlash(filepath.Join(module, rel)), nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", errors.New("go.mod not found for " + folder)
}

func readModulePath(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}

	return "", errors.New("module path not found in " + path)
}
