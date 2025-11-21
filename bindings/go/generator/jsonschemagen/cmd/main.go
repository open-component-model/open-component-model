package main

import (
	"fmt"
	"log"
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
		log.Fatalf("Usage: jsonschemagen <root-dir>")
	}

	roots := os.Args[1:]

	for i, root := range roots {
		var err error
		if roots[i], err = filepath.Abs(root); err != nil {
			log.Fatalf("cannot resolve root directory: %v", err)
		}
	}

	fmt.Printf("jsonschemagen: scanning %s …\n", roots)

	///////////////////////////////////////////////////////////////////////////////
	// 1. Build Universe (discover all types + imports)
	///////////////////////////////////////////////////////////////////////////////

	u, err := universe.Build(roots)
	if err != nil {
		log.Fatalf("universe build failed: %v", err)
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
		fmt.Println("No annotated types found (+ocm:jsonschema-gen=true). Nothing to do.")
		return
	}

	fmt.Printf("Discovered %d annotated types.\n", len(annotated))

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
			log.Printf("WARNING: schema generation returned nil for %s", ti.Key.TypeName)
			continue
		}

		if err := writer.WriteSchemaJSON(ti, schema); err != nil {
			log.Fatalf("write schema error for %s: %v", ti.Key.TypeName, err)
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
			log.Fatalf("embed file error for package %s: %v", pkgDir, err)
		}
	}

	fmt.Println("jsonschemagen: completed successfully.")
}
