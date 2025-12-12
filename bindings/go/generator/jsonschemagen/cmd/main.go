package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"golang.org/x/tools/go/packages"
	"ocm.software/open-component-model/bindings/go/generator/jsonschemagen"
	"ocm.software/open-component-model/bindings/go/generator/jsonschemagen/writer"
	"ocm.software/open-component-model/bindings/go/generator/universe"
)

func main() {
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

	if err := Run(context.Background(), args...); err != nil {
		slog.Error("jsonschemagen failed", "error", err)
		os.Exit(1)
	}
}

func Run(ctx context.Context, roots ...string) error {
	for i, root := range roots {
		abs, err := filepath.Abs(root)
		if err != nil {
			return fmt.Errorf("cannot resolve root directory %q: %w", root, err)
		}
		roots[i] = abs
	}

	slog.Info("scanning...", "roots", roots)

	// Build the universe of all discovered types and imports.
	u, err := universe.Build(ctx, roots)
	if err != nil {
		return fmt.Errorf("universe build error: %w", err)
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
		return nil
	}
	slog.Info("discovered annotated types", "types", len(annotated), "baseMarker", jsonschemagen.BaseMarker)

	// Initialize the schema generator.
	gen := jsonschemagen.New(u)

	// GenerateJSONSchemaDraft202012 schemas for all annotated types.
	groups := make(map[*packages.Package][]*universe.TypeInfo)
	for _, ti := range annotated {
		if _, ok := jsonschemagen.SchemaFromUniverseType(ti); ok {
			slog.Debug("skipping schema generation, schema provided from type", "type", ti.Key.TypeName)
			groups[ti.Pkg] = append(groups[ti.Pkg], ti)
			continue
		}

		schema := gen.GenerateJSONSchemaDraft202012(ti)
		if schema == nil {
			slog.Warn("schema generation returned nil", "type", ti.Key.TypeName)
			continue
		}

		if err := writer.WriteSchemaJSON(ti, schema); err != nil {
			return fmt.Errorf("write schema error for type %q: %w", ti.Key.TypeName, err)
		}

		groups[ti.Pkg] = append(groups[ti.Pkg], ti)
	}

	for pkg, group := range groups {
		writeEmbedFiles(pkg, group)
	}

	slog.Info("jsonschemagen: completed successfully.")
	return nil
}

func writeEmbedFiles(pkg *packages.Package, group []*universe.TypeInfo) {
	if len(group) == 0 {
		return
	}
	// Types from the same directory share a package name.

	embedInfos := make([]writer.TypeEmbedInfo, 0, len(group))
	for _, ti := range group {
		var embedSchemaPath string
		if pathFromType, ok := jsonschemagen.SchemaFromUniverseType(ti); ok {
			// Schema is provided from type itself, skip generation here.
			embedSchemaPath = pathFromType
		} else {
			embedSchemaPath = fmt.Sprintf("schemas/%s.schema.json", ti.Key.TypeName)
		}
		embedInfos = append(embedInfos, writer.TypeEmbedInfo{
			Key:             ti.Key,
			EmbedSchemaPath: embedSchemaPath,
		})
	}

	if err := writer.WriteEmbedFileForPackage(pkg.Dir, pkg.Types.Name(), embedInfos); err != nil {
		slog.Error("write embed file error", "dir", pkg.Dir, "error", err)
		os.Exit(1)
	}
}
