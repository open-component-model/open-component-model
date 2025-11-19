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

///////////////////////////////////////////////////////////////////////////
// Logging
///////////////////////////////////////////////////////////////////////////

var logLevel slog.LevelVar

var Logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
	Level: &logLevel,
}))

func init() {
	logLevel.Set(getLogLevelFromEnv())
}

func getLogLevelFromEnv() slog.Level {
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
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

///////////////////////////////////////////////////////////////////////////
// Constants
///////////////////////////////////////////////////////////////////////////

const (
	marker             = "+ocm:jsonschema-gen=true"
	SchemaDir          = "schemas"
	outputGo           = "zz_generated_schemas.go"
	runtimeTypePattern = `^([a-zA-Z0-9][a-zA-Z0-9.]*)(?:/(v[0-9]+(?:alpha[0-9]+|beta[0-9]+)?))?$`
)

///////////////////////////////////////////////////////////////////////////
// Main
///////////////////////////////////////////////////////////////////////////

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

///////////////////////////////////////////////////////////////////////////
// Struct models
///////////////////////////////////////////////////////////////////////////

type StructInfo struct {
	PkgPath     string
	PkgName     string
	TypeName    string
	File        string
	Node        *ast.StructType
	TypeComment string
}

type Schema struct {
	Schema               string             `json:"$schema,omitempty"`
	ID                   string             `json:"$id,omitempty"`
	Description          string             `json:"description,omitempty"`
	Title                string             `json:"title,omitempty"`
	Type                 string             `json:"type,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	AdditionalProperties *SchemaOrBool      `json:"additionalProperties,omitempty"`
	Pattern              string             `json:"pattern,omitempty"`
	Required             []string           `json:"required,omitempty"`
}

type SchemaOrBool struct {
	Schema *Schema `json:"schema,omitempty"`
	Bool   *bool   `json:"bool,omitempty"`
}

func (s *SchemaOrBool) MarshalJSON() ([]byte, error) {
	if s.Bool != nil {
		return json.Marshal(s.Bool)
	}
	return json.Marshal(s.Schema)
}

///////////////////////////////////////////////////////////////////////////
// Annotated type registry
///////////////////////////////////////////////////////////////////////////

var annotatedTypes = map[string]StructInfo{}

func lookupStruct(name string) (*StructInfo, bool) {
	for k, si := range annotatedTypes {
		if strings.HasSuffix(k, "."+name) {
			return &si, true
		}
	}
	return nil, false
}

///////////////////////////////////////////////////////////////////////////
// Find annotated structs
///////////////////////////////////////////////////////////////////////////

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

				comment := findStructComment(ts, gd)

				si := StructInfo{
					PkgPath:     importPath,
					PkgName:     file.Name.Name,
					TypeName:    ts.Name.Name,
					File:        path,
					Node:        st,
					TypeComment: comment,
				}

				out = append(out, si)
				annotatedTypes[fmt.Sprintf("%s.%s", importPath, ts.Name.Name)] = si
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

///////////////////////////////////////////////////////////////////////////
// go.mod import path
///////////////////////////////////////////////////////////////////////////

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
		if parent := filepath.Dir(dir); parent != dir {
			dir = parent
			continue
		}
		break
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

///////////////////////////////////////////////////////////////////////////
// Schema generation
///////////////////////////////////////////////////////////////////////////

func GenerateSchemaForStruct(info StructInfo, schemaID string) *Schema {
	props := make(map[string]*Schema)
	var required []string

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

		sch, err := schemaForField(field.Type, map[string]bool{})
		if err != nil {
			panic(err)
		}

		comment := findFieldComment(field)
		if comment != "" {
			sch.Description = comment
		}

		props[jsonName] = sch

		if !jsonTagHasOmitEmpty(field) {
			required = append(required, jsonName)
		}
	}

	return &Schema{
		Schema:      "https://json-schema.org/draft/2020-12/schema",
		ID:          schemaID,
		Title:       info.TypeName,
		Description: info.TypeComment,
		Type:        "object",
		Properties:  props,
		Required:    required,
	}
}

///////////////////////////////////////////////////////////////////////////
// Recursive schema creation
///////////////////////////////////////////////////////////////////////////

func schemaForField(expr ast.Expr, stack map[string]bool) (*Schema, error) {
	switch t := expr.(type) {

	// runtime.Type
	case *ast.SelectorExpr:
		if isRuntimeType(t) {
			return &Schema{
				Type:    "string",
				Pattern: runtimeTypePattern,
			}, nil
		}
		return &Schema{Type: "object"}, nil

	// Identifiers (primitive or struct)
	case *ast.Ident:
		if ps := primitiveSchema(t.Name); ps.Type != "object" {
			return ps, nil
		}

		if si, ok := lookupStruct(t.Name); ok {
			if stack[si.TypeName] {
				return nil, fmt.Errorf("recursive type detected: %s", si.TypeName)
			}

			stack[si.TypeName] = true
			defer delete(stack, si.TypeName)

			props := make(map[string]*Schema)
			var required []string

			for _, f := range si.Node.Fields.List {
				if len(f.Names) == 0 {
					continue
				}

				fn := f.Names[0].Name
				tag := parseJSONTag(f)
				jsonName := fn
				if tag != "" {
					jsonName = tag
				}

				sub, err := schemaForField(f.Type, stack)
				if err != nil {
					return nil, err
				}
				props[jsonName] = sub

				if !jsonTagHasOmitEmpty(f) {
					required = append(required, jsonName)
				}
			}

			return &Schema{
				Type:       "object",
				Properties: props,
				Required:   required,
				AdditionalProperties: &SchemaOrBool{
					Bool: ptr(false),
				},
			}, nil
		}

		return &Schema{Type: "object"}, nil

	// Anonymous struct
	case *ast.StructType:
		props := make(map[string]*Schema)
		var required []string

		for _, f := range t.Fields.List {
			if len(f.Names) == 0 {
				continue
			}
			fn := f.Names[0].Name
			tag := parseJSONTag(f)

			jsonName := fn
			if tag != "" {
				jsonName = tag
			}

			sub, err := schemaForField(f.Type, stack)
			if err != nil {
				return nil, err
			}

			props[jsonName] = sub
			required = append(required, jsonName)
		}

		return &Schema{
			Type:                 "object",
			Properties:           props,
			Required:             required,
			AdditionalProperties: &SchemaOrBool{Bool: ptr(false)},
		}, nil

	// Array
	case *ast.ArrayType:
		s, err := schemaForField(t.Elt, stack)
		if err != nil {
			return nil, err
		}
		return &Schema{Type: "array", Items: s}, nil

	// Map
	case *ast.MapType:
		if isAnyType(t.Value) {
			return &Schema{
				Type: "object",
				AdditionalProperties: &SchemaOrBool{
					Bool: ptr(true),
				},
			}, nil
		}

		sub, err := schemaForField(t.Value, stack)
		if err != nil {
			return nil, err
		}

		return &Schema{
			Type: "object",
			AdditionalProperties: &SchemaOrBool{
				Schema: sub,
			},
		}, nil

	// *runtime.Raw → free-form object with required type field
	case *ast.StarExpr:
		if isRuntimeRaw(t) {
			return &Schema{
				Type: "object",
				Properties: map[string]*Schema{
					"type": {
						Type:    "string",
						Pattern: runtimeTypePattern,
					},
				},
				Required:             []string{"type"},
				AdditionalProperties: &SchemaOrBool{Bool: ptr(true)},
			}, nil
		}
		return schemaForField(t.X, stack)
	}

	return &Schema{Type: "object"}, nil
}

///////////////////////////////////////////////////////////////////////////
// Helpers
///////////////////////////////////////////////////////////////////////////

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

func isAnyType(expr ast.Expr) bool {
	switch x := expr.(type) {
	case *ast.InterfaceType:
		return true
	case *ast.Ident:
		return x.Name == "any" || x.Name == "interface{}"
	default:
		return false
	}
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

///////////////////////////////////////////////////////////////////////////
// JSON tag parsing
///////////////////////////////////////////////////////////////////////////

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
		if !strings.HasPrefix(part, `json:"`) {
			continue
		}

		content := strings.TrimSuffix(strings.TrimPrefix(part, `json:"`), `"`)
		parts := strings.Split(content, ",")
		for _, p := range parts[1:] {
			if p == "omitempty" {
				return true
			}
		}
	}
	return false
}

///////////////////////////////////////////////////////////////////////////
// Comments
///////////////////////////////////////////////////////////////////////////

func extractCommentText(cg *ast.CommentGroup) string {
	if cg == nil {
		return ""
	}

	var out []string
	for _, c := range cg.List {
		x := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
		x = strings.TrimSpace(strings.TrimPrefix(x, "/*"))
		x = strings.TrimSpace(strings.TrimSuffix(x, "*/"))
		if x != "" {
			out = append(out, x)
		}
	}
	return cleanDescription(strings.Join(out, "\n"))
}

func findFieldComment(f *ast.Field) string {
	return cleanDescription(extractCommentText(f.Doc))
}

func findStructComment(ts *ast.TypeSpec, gd *ast.GenDecl) string {
	if ts.Doc != nil {
		return extractCommentText(ts.Doc)
	}
	return extractCommentText(gd.Doc)
}

func cleanDescription(desc string) string {
	if desc == "" {
		return ""
	}
	var out []string
	for _, line := range strings.Split(desc, "\n") {
		if strings.HasPrefix(line, "+") {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

///////////////////////////////////////////////////////////////////////////
// Output writers
///////////////////////////////////////////////////////////////////////////

func WriteSchemaJSON(info StructInfo, schema *Schema) {
	outDir := filepath.Join(filepath.Dir(info.File), SchemaDir)
	_ = os.MkdirAll(outDir, 0o755)

	raw, _ := json.MarshalIndent(schema, "", "  ")
	out := filepath.Join(outDir, info.TypeName+".json")

	if err := os.WriteFile(out, raw, 0o644); err != nil {
		panic(err)
	}
}

func WriteEmbedFile(infos []StructInfo) {
	if len(infos) == 0 {
		return
	}

	dir := filepath.Dir(infos[0].File)
	outPath := filepath.Join(dir, outputGo)

	Logger.Debug("writing schema embed file", "file", outPath, "pkg", infos[0].PkgPath)

	f, err := os.Create(outPath)
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

///////////////////////////////////////////////////////////////////////////
// Utility
///////////////////////////////////////////////////////////////////////////

func ptr[T any](v T) *T { return &v }
