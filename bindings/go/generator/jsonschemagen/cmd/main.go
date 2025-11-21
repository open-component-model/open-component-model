package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"ocm.software/open-component-model/bindings/go/generator/jsonschemagen"
	"ocm.software/open-component-model/bindings/go/generator/jsonschemagen/writer"
	"ocm.software/open-component-model/bindings/go/generator/universe"
)

const marker = "+ocm:jsonschema-gen=true"

func main() {
	if len(os.Args) < 2 {
		slog.Error("Usage: jsonschemagen <root-dir>")
		os.Exit(1)
	}

	roots := os.Args[1:]

	for i, root := range roots {
		var err error
		if roots[i], err = filepath.Abs(root); err != nil {
			slog.Error("cannot resolve root directory", "root", root, "error", err)
			os.Exit(1)
		}
	}

	slog.Info("scanning...", "roots", roots)

	///////////////////////////////////////////////////////////////////////////////
	// 1. Build Universe (discover all types + imports)
	///////////////////////////////////////////////////////////////////////////////

	u, err := universe.Build(roots)
	if err != nil {
		slog.Error("universe build failed", "error", err)
		os.Exit(1)
	}

	///////////////////////////////////////////////////////////////////////////////
	// 2. Filter annotated types (annotation is detected by generator)
	///////////////////////////////////////////////////////////////////////////////

	var annotated []*universe.TypeInfo

	for _, ti := range u.Types {
		if jsonschemagen.HasMarker(ti.TypeSpec, ti.GenDecl, marker) {
			annotated = append(annotated, ti)
		}
	}

	if len(annotated) == 0 {
		slog.Warn("No annotated types found. Nothing to do.", "marker", marker)
		return
	}
	slog.Info("discovered annotated types", "types", annotated, "marker", marker)

	///////////////////////////////////////////////////////////////////////////////
	// 3. Initialize Generator
	///////////////////////////////////////////////////////////////////////////////

	gen := jsonschemagen.New(u)

	///////////////////////////////////////////////////////////////////////////////
	// 4. Generate schemas for all annotated types
	///////////////////////////////////////////////////////////////////////////////

	packageGroups := map[string][]*universe.TypeInfo{}

	for _, ti := range annotated {
		schema := gen.Generate(ti)
		if schema == nil {
			slog.Warn("schema generation returned nil", "type", ti.Key.TypeName)
			continue
		}

		if err := writer.WriteSchemaJSON(ti, schema); err != nil {
			slog.Error("write schema error", "type", ti.Key.TypeName, "error", err)
			os.Exit(1)
		}

		pkgDir := filepath.Dir(ti.FilePath)
		packageGroups[pkgDir] = append(packageGroups[pkgDir], ti)
	}

	///////////////////////////////////////////////////////////////////////////////
	// 5. Generate embed files per package
	///////////////////////////////////////////////////////////////////////////////

	dirs := make([]string, 0, len(packageGroups))
	for d := range packageGroups {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	for _, pkgDir := range dirs {
		types := packageGroups[pkgDir]
		if len(types) == 0 {
			continue
		}

		// all types in the same dir share a package name
		pkgName := types[0].File.Name.Name

		if err := writer.WriteEmbedFileForPackage(pkgDir, pkgName, types); err != nil {
			slog.Error("write embed file error", "dir", pkgDir, "error", err)
			os.Exit(1)
		}
	}

	slog.Info("jsonschemagen: completed successfully.")
}
