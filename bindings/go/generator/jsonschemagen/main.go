package main

import (
	"bufio"
	"encoding/json"
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

var Logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
	Level: &logLevel,
}))

var logLevel slog.LevelVar // INFO is the zero value
// the initial value is set from the environment and you can call Set() anytime
// to update this value
func init() {
	logLevel.Set(getLogLevelFromEnv())
}

func getLogLevelFromEnv() slog.Level {
	levelStr := os.Getenv("LOG_LEVEL")

	switch strings.ToLower(levelStr) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func main() {
	if len(os.Args) < 2 {
		Logger.Error("Usage: jsonschemagen <root-dir>")
		os.Exit(1)
	}
	root := os.Args[1]

	infos, err := FindAnnotatedStructs(root)
	if err != nil {
		Logger.Error("scan error", "error", err)
		os.Exit(1)
	}
	if len(infos) == 0 {
		Logger.Info("No annotated types found")
		return
	}

	for _, info := range infos {
		schemaID := fmt.Sprintf("%s/%s/%s.json", info.PkgPath, SchemaDir, info.TypeName)
		schema := GenerateSchemaForStruct(info, schemaID)
		WriteSchemaJSON(info, schema)
	}

	WriteEmbedFile(infos)
}

const (
	marker    = "+ocm:jsonschema-gen=true"
	SchemaDir = "schemas"
	outputGo  = "zz_generated_schemas.go"
)

type StructInfo struct {
	PkgPath  string
	PkgName  string
	TypeName string
	File     string
	Node     *ast.StructType
}

type Schema struct {
	Schema               string             `json:"$schema,omitempty"`
	ID                   string             `json:"$id,omitempty"`
	Title                string             `json:"title,omitempty"`
	Type                 string             `json:"type,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	AdditionalProperties *Schema            `json:"additionalProperties,omitempty"`
	Pattern              string             `json:"pattern,omitempty"`
	Required             []string           `json:"required,omitempty"`
	Description          string             `json:"description,omitempty"`
}

func FindAnnotatedStructs(root string) ([]StructInfo, error) {
	var out []StructInfo

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		if !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") ||
			strings.HasPrefix(filepath.Base(path), "zz_generated") {
			return nil
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}

		importPath, _ := getImportPath(filepath.Dir(path))

		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, s := range gd.Specs {
				ts, ok := s.(*ast.TypeSpec)
				if !ok || !hasMarker(gd.Doc, ts.Doc) {
					continue
				}

				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}

				out = append(out, StructInfo{
					PkgPath:  importPath,
					PkgName:  file.Name.Name,
					TypeName: ts.Name.Name,
					File:     path,
					Node:     st,
				})
			}
		}

		return nil
	})

	return out, err
}

func hasMarker(groups ...*ast.CommentGroup) bool {
	for _, g := range groups {
		if g == nil {
			continue
		}
		for _, c := range g.List {
			if strings.Contains(c.Text, marker) {
				return true
			}
		}
	}
	return false
}

////////////////////////////////////////////////////////////////////////////////
// GO.MOD MODULE PATH
////////////////////////////////////////////////////////////////////////////////

func getImportPath(folder string) (string, error) {
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
	return "", errors.New("go.mod not found")
}

func readModulePath(path string) (string, error) {
	f, _ := os.Open(path)
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	return "", errors.New("module path not found")
}

func GenerateSchemaForStruct(info StructInfo, schemaID string) *Schema {
	props := map[string]*Schema{}
	required := []string{}

	for _, field := range info.Node.Fields.List {
		if len(field.Names) == 0 {
			continue
		}

		name := field.Names[0].Name
		tag := parseJSONTag(field)

		if tag == "-" {
			continue
		}

		jsonName := name
		if tag != "" {
			jsonName = tag
		}

		// field schema
		s := schemaForField(field.Type)
		props[jsonName] = s

		// required = NOT omitempty
		if !jsonTagHasOmitEmpty(field) {
			required = append(required, jsonName)
		}
	}

	return &Schema{
		Schema:     "https://json-schema.org/draft/2020-12/schema",
		ID:         schemaID,
		Title:      info.TypeName,
		Type:       "object",
		Properties: props,
		Required:   required,
	}
}

func schemaForField(expr ast.Expr) *Schema {
	switch t := expr.(type) {

	case *ast.Ident:
		return primitiveSchema(t.Name)

	case *ast.SelectorExpr:
		if isRuntimeType(t) {
			return &Schema{
				Type:    "string",
				Pattern: `^([a-zA-Z0-9][a-zA-Z0-9.]*)(?:/(v[0-9]+(?:alpha[0-9]+|beta[0-9]+)?))?$`,
			}
		}
		return &Schema{Type: "object"}

	case *ast.StructType:
		props := map[string]*Schema{}
		required := []string{}
		for _, f := range t.Fields.List {
			if len(f.Names) == 0 {
				continue
			}
			subName := f.Names[0].Name
			tag := parseJSONTag(f)
			jsonName := subName
			if tag != "" {
				jsonName = tag
			}
			props[jsonName] = schemaForField(f.Type)
			required = append(required, jsonName)
		}
		return &Schema{
			Type:       "object",
			Properties: props,
			Required:   required,
		}

	case *ast.ArrayType:
		return &Schema{
			Type:  "array",
			Items: schemaForField(t.Elt),
		}

	case *ast.MapType:
		return &Schema{
			Type:                 "object",
			AdditionalProperties: schemaForField(t.Value),
		}

	case *ast.StarExpr:
		if isRuntimeRaw(t) {
			return &Schema{} // free-form
		}
		return schemaForField(t.X)
	}

	return &Schema{Type: "object"}
}

func primitiveSchema(name string) *Schema {
	switch name {
	case "string":
		return &Schema{Type: "string"}
	case "int", "int32", "int64":
		return &Schema{Type: "integer"}
	case "float32", "float64":
		return &Schema{Type: "number"}
	case "bool":
		return &Schema{Type: "boolean"}
	}
	return &Schema{Type: "object"}
}

func isRuntimeType(x *ast.SelectorExpr) bool {
	ident, ok := x.X.(*ast.Ident)
	return ok && ident.Name == "runtime" && x.Sel.Name == "Type"
}

func isRuntimeRaw(x *ast.StarExpr) bool {
	sel, ok := x.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == "runtime" && sel.Sel.Name == "Raw"
}

////////////////////////////////////////////////////////////////////////////////
// JSON TAG PARSER
////////////////////////////////////////////////////////////////////////////////

func parseJSONTag(f *ast.Field) string {
	if f.Tag == nil {
		return ""
	}
	tag := strings.Trim(f.Tag.Value, "`")
	for _, part := range strings.Split(tag, " ") {
		if strings.HasPrefix(part, "json:\"") {
			val := strings.TrimPrefix(part, `json:"`)
			val = strings.TrimSuffix(val, `"`)
			return strings.Split(val, ",")[0]
		}
	}
	return ""
}

func jsonTagHasOmitEmpty(f *ast.Field) bool {
	if f.Tag == nil {
		return false
	}
	tag := strings.Trim(f.Tag.Value, "`")
	for _, part := range strings.Split(tag, " ") {
		if strings.HasPrefix(part, `json:"`) {
			content := strings.TrimPrefix(part, `json:"`)
			content = strings.TrimSuffix(content, `"`)
			parts := strings.Split(content, ",")
			for _, p := range parts[1:] { // skip field name
				if p == "omitempty" {
					return true
				}
			}
		}
	}
	return false
}

func WriteSchemaJSON(info StructInfo, schema *Schema) {
	outDir := filepath.Join(filepath.Dir(info.File), SchemaDir)
	_ = os.MkdirAll(outDir, 0o755)

	raw, _ := json.MarshalIndent(schema, "", "  ")
	filename := filepath.Join(outDir, info.TypeName+".json")

	if err := os.WriteFile(filename, raw, 0o644); err != nil {
		panic(err)
	}
}

func WriteEmbedFile(infos []StructInfo) {
	if len(infos) == 0 {
		return
	}
	dir := filepath.Dir(infos[0].File)
	outPath := filepath.Join(dir, outputGo)

	Logger.Debug("writing schema embed file",
		"file", outPath,
		"pkg", infos[0].PkgPath,
	)

	f, err := os.Create(filepath.Join(dir, outputGo))
	if err != nil {
		panic(err)
	}
	defer f.Close()

	fmt.Fprintf(f, `//go:build !ignore_autogenerated

package %s

import "embed"

//go:embed %s/*.json
var JSONSchemaFS embed.FS
`, infos[0].PkgName, SchemaDir)
}
