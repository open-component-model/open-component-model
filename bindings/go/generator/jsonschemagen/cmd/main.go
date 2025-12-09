package main

import (
	"flag"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"ocm.software/open-component-model/bindings/go/generator/jsonschemagen"
	"ocm.software/open-component-model/bindings/go/generator/jsonschemagen/writer"
	"ocm.software/open-component-model/bindings/go/generator/universe"
)

func main() {
	// -------------------------------
	// Logging flag
	// -------------------------------
	logLevelFlag := flag.String("loglevel", "info", "debug, info, warn, error")
	flag.Parse()

	// Apply log level
	level := slog.LevelInfo
	switch *logLevelFlag {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		// keep default info
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})))

	// -------------------------------
	// Root argument check
	// -------------------------------
	args := flag.Args()
	if len(args) < 1 {
		slog.Error("Usage: jsonschemagen [-loglevel=LEVEL] <root-dir> [<root-dir>...] [--help]")
		os.Exit(1)
	}

	roots := args
	for i, root := range roots {
		abs, err := filepath.Abs(root)
		if err != nil {
			slog.Error("cannot resolve root directory", "root", root, "error", err)
			os.Exit(1)
		}
		roots[i] = abs
	}

	slog.Info("scanning...", "roots", roots)

	// Build the universe of all discovered types and imports.
	u, err := universe.Build(roots)
	if err != nil {
		slog.Error("universe build failed", "error", err)
		os.Exit(1)
	}

	// Collect all types annotated for schema generation.
	var annotated []*universe.TypeInfo
	for _, ti := range u.Types {
		if jsonschemagen.HasMarkerKey(ti.TypeSpec, ti.GenDecl, jsonschemagen.BaseMarker) {
			annotated = append(annotated, ti)
		}
	}

	if len(annotated) == 0 {
		slog.Warn("No annotated types found. Nothing to do.", "baseMarker", jsonschemagen.BaseMarker)
		return
	}
	slog.Info("discovered annotated types", "types", len(annotated), "baseMarker", jsonschemagen.BaseMarker)

	// Initialize the schema generator.
	gen := jsonschemagen.New(u)

	// GenerateJSONSchemaDraft202012 schemas for all annotated types.
	packageGroups := make(map[string][]*universe.TypeInfo)
	for _, ti := range annotated {
		markers := jsonschemagen.ExtractMarkerMap(ti.TypeSpec, ti.GenDecl, jsonschemagen.BaseMarker)
		if schema, ok := jsonschemagen.SchemaFromMarker(markers); ok {
			sch := &jsonschemagen.JSONSchemaDraft202012{}
			jsonschemagen.ApplyFileMarkers(sch, schema, ti.FilePath)
		} else {
			schema := gen.GenerateJSONSchemaDraft202012(ti)
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

	}

	// Generate embed files for each package that contains generated schemas.
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

		// Types from the same directory share a package name.
		pkgName := types[0].File.Name.Name

		if err := writer.WriteEmbedFileForPackage(pkgDir, pkgName, types); err != nil {
			slog.Error("write embed file error", "dir", pkgDir, "error", err)
			os.Exit(1)
		}
	}

	slog.Info("jsonschemagen: completed successfully.")
}
