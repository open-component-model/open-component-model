package main

import (
	"bufio"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const (
	// typegenMarker is the marker used to identify types for code generation.
	// It should be present in the comments of the type declaration.
	// The marker is used to indicate that the type should be processed by the generator.
	// The marker is expected to be in the format: "+ocm:typegen=true"
	typegenMarker = "+ocm:typegen=true"
	// generatedFile is the name of the generated file.
	// The generator will create this file in the same package directory as the source files.
	// The file will contain the generated code for the types marked for generation.
	generatedFile = "zz_generated.ocm_type.go"
	// runtimeImport is the import path for the `runtime` package.
	// This package contains the `Type` struct that is used in the generated code.
	// The generator will ensure that this import is included in the generated file,
	// if the package is not the same as the runtime package.
	runtimeImport = "ocm.software/open-component-model/bindings/go/runtime"
	// runtimeTypeFieldName is the name of the field in the struct that holds the type information.
	// This field must be of type `runtime.Type` for the generator to process the struct.
	runtimeTypeFieldName = "Type"
)

var Logger = slog.Default()

func init() {
	var level slog.Level
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	slog.SetLogLoggerLevel(level)
}

func main() {
	if len(os.Args) < 2 {
		slog.Info("Usage: generator <root-folder>")
		os.Exit(1)
	}
	root := os.Args[1]

	packages, err := findGoPackages(root)
	if err != nil {
		slog.Error("Failed to find go packages", "error", err)
		os.Exit(1)
	}

	for _, pkgDir := range packages {
		pkgName, types, err := scanSinglePackage(pkgDir)
		if err != nil {
			slog.Error("error scanning", "dir", "pkgDir", "error", err)
			continue
		}
		if len(types) == 0 {
			continue
		}
		slog.Debug("Generating", "pkg", pkgName, "dir", pkgDir, "types", types)

		err = generateCode(pkgDir, pkgName, types)
		if err != nil {
			slog.Error("Error generating", "pkg", pkgName, "dir", pkgDir, "error", err)
		}
	}
}

// scanSinglePackage inspects a folder for Go type definitions marked for code generation.
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
					slog.Info("skipping type", "name", typeSpec.Name.Name, "reason", "not a struct with runtime.Type field")
					continue
				}

				typesToGenerate = append(typesToGenerate, typeSpec.Name.Name)
			}
		}
	}

	return packageName, typesToGenerate, nil
}

// findGoPackages recursively walks a directory to find all folders containing valid Go files.
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

// isValidGoFile checks if a file should be considered for parsing.
func isValidGoFile(name string) bool {
	return strings.HasSuffix(name, ".go") &&
		!strings.HasSuffix(name, "_test.go") &&
		!strings.HasPrefix(name, "zz_generated.")
}

// hasMarker returns true if any comment group contains the typegen marker.
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

// hasRuntimeTypeField checks if the struct has a field named Type of type runtime.Type.
func hasRuntimeTypeField(s *ast.StructType) bool {
	for _, field := range s.Fields.List {
		for _, name := range field.Names {
			if name.Name == runtimeTypeFieldName {
				if sel, ok := field.Type.(*ast.SelectorExpr); ok {
					if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "runtime" && sel.Sel.Name == runtimeTypeFieldName {
						return true
					}
				}
			}
		}
	}
	return false
}

// getImportPath returns the Go import path for a given folder by reading the nearest go.mod file.
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

// readModulePath reads the module path declared in a go.mod file.
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

// generateCode creates a file with SetType and GetType methods for the provided types.
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

// Code generated by ocmtypegen. DO NOT EDIT.`)
	fmt.Fprintf(out, "\npackage %s\n\n", pkg)

	if importPath != runtimeImport {
		fmt.Fprintf(out, "import \"%s\"\n\n", runtimeImport)
	}

	for i, name := range types {
		if i > 0 {
			fmt.Fprintf(out, "\n")
		}
		fmt.Fprintf(out, "// SetType is an autogenerated setter function, useful for type inference and defaulting.\n")
		fmt.Fprintf(out, "func (t *%s) SetType(typ runtime.Type) {\n\tt.Type = typ\n}\n\n", name)
		fmt.Fprintf(out, "// GetType is an autogenerated getter function, useful for type inference and defaulting.\n")
		fmt.Fprintf(out, "func (t *%s) GetType() runtime.Type {\n\treturn t.Type\n}\n", name)
	}
	return nil
}
