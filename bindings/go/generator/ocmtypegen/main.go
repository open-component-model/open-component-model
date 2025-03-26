package main

import (
	"bufio"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

const (
	typegenMarker = "+ocm:typegen=true"
	generatedFile = "zz_generated.ocm_type.go"
	runtimeImport = "ocm.software/open-component-model/bindings/go/runtime"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: generator <root-folder>")
		os.Exit(1)
	}
	root := os.Args[1]

	packages, err := findGoPackages(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error finding Go packages: %v\n", err)
		os.Exit(1)
	}

	for _, pkgDir := range packages {
		// Important: scanFolder must only look inside this one subpackage directory
		pkgName, types, err := scanSinglePackage(pkgDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error scanning %s: %v\n", pkgDir, err)
			continue
		}
		if len(types) == 0 {
			continue
		}
		fmt.Printf("Generating for %s (%s): %v\n", pkgName, pkgDir, types)

		err = generateCode(pkgDir, pkgName, types)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error generating in %s: %v\n", pkgDir, err)
		}
	}
}

func scanSinglePackage(folder string) (string, []string, error) {
	fset := token.NewFileSet()
	var packageName string
	var typesToGenerate []string

	files, err := os.ReadDir(folder)
	if err != nil {
		return "", nil, err
	}

	for _, f := range files {
		if f.IsDir() || !isValidGoFile(f.Name()) {
			continue
		}

		fullPath := filepath.Join(folder, f.Name())
		file, err := parser.ParseFile(fset, fullPath, nil, parser.ParseComments)
		if err != nil {
			return "", nil, err
		}

		if packageName == "" {
			packageName = file.Name.Name
		}

		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.TYPE {
				continue
			}

			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok || !hasMarker(genDecl.Doc, typeSpec.Doc) {
					continue
				}

				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok || !hasRuntimeTypeField(structType) {
					continue
				}

				typesToGenerate = append(typesToGenerate, typeSpec.Name.Name)
			}
		}
	}

	return packageName, typesToGenerate, nil
}

func findGoPackages(root string) ([]string, error) {
	var packages []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return err
		}
		files, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, file := range files {
			if !file.IsDir() && isValidGoFile(file.Name()) {
				packages = append(packages, path)
				break
			}
		}
		return nil
	})
	return packages, err
}

func scanFolder(folder string) (string, []string, error) {
	fset := token.NewFileSet()
	var packageName string
	var typesToGenerate []string

	err := filepath.Walk(folder, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !isValidGoFile(info.Name()) {
			return err
		}

		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		if packageName == "" {
			packageName = file.Name.Name
		}

		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.TYPE {
				continue
			}

			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok || !hasMarker(genDecl.Doc, typeSpec.Doc) {
					continue
				}
				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok || !hasRuntimeTypeField(structType) {
					continue
				}
				typesToGenerate = append(typesToGenerate, typeSpec.Name.Name)
			}
		}
		return nil
	})

	return packageName, typesToGenerate, err
}

func isValidGoFile(name string) bool {
	return strings.HasSuffix(name, ".go") &&
		!strings.HasSuffix(name, "_test.go") &&
		!strings.HasPrefix(name, "zz_generated.")
}

func hasMarker(groups ...*ast.CommentGroup) bool {
	for _, g := range groups {
		if g == nil {
			continue
		}
		for _, c := range g.List {
			if strings.Contains(strings.TrimSpace(c.Text), typegenMarker) {
				return true
			}
		}
	}
	return false
}

func hasRuntimeTypeField(s *ast.StructType) bool {
	for _, field := range s.Fields.List {
		for _, name := range field.Names {
			if name.Name == "Type" {
				if sel, ok := field.Type.(*ast.SelectorExpr); ok {
					if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "runtime" && sel.Sel.Name == "Type" {
						return true
					}
				}
			}
		}
	}
	return false
}

func getImportPath(folder string) (string, error) {
	absFolder, err := filepath.Abs(folder)
	if err != nil {
		return "", err
	}

	dir := absFolder
	for {
		goModPath := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(goModPath); err == nil {
			modulePath, err := readModulePath(goModPath)
			if err != nil {
				return "", err
			}
			relPath, err := filepath.Rel(dir, absFolder)
			if err != nil {
				return "", err
			}
			if relPath == "." {
				return modulePath, nil
			}
			return filepath.ToSlash(filepath.Join(modulePath, relPath)), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", errors.New("go.mod not found")
}

func readModulePath(goModPath string) (string, error) {
	file, err := os.Open(goModPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	return "", errors.New("module path not found")
}

func generateCode(folder, pkg string, types []string) error {
	outputPath := filepath.Join(folder, generatedFile)
	out, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer out.Close()

	importPath, err := getImportPath(folder)
	if err != nil {
		return fmt.Errorf("failed to determine import path: %w", err)
	}

	fmt.Fprintln(out, `//go:build !ignore_autogenerated
// +build !ignore_autogenerated

// Code generated by typegen. DO NOT EDIT.`)
	fmt.Fprintf(out, "\npackage %s\n\n", pkg)

	if importPath != runtimeImport {
		fmt.Fprintf(out, "import \"%s\"\n\n", runtimeImport)
	}

	for _, name := range types {
		fmt.Fprintf(out, "func (t *%s) SetType(typ runtime.Type) {\n\tt.Type = typ\n}\n\n", name)
		fmt.Fprintf(out, "func (t *%s) GetType() runtime.Type {\n\treturn t.Type\n}\n\n", name)
	}
	return nil
}
