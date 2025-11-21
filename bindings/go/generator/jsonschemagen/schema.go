package jsonschemagen

import (
	"go/ast"

	"ocm.software/open-component-model/bindings/go/generator/universe"
)

///////////////////////////////////////////////////////////////////////////////
// JSON Schema model
///////////////////////////////////////////////////////////////////////////////

type Schema struct {
	Schema      string `json:"$schema,omitempty"`
	ID          string `json:"$id,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type,omitempty"`
	Ref         string `json:"$ref,omitempty"`

	Properties map[string]*Schema `json:"properties,omitempty"`
	Items      *Schema            `json:"items,omitempty"`
	Required   []string           `json:"required,omitempty"`

	AdditionalProperties *SchemaOrBool `json:"additionalProperties,omitempty"`
	Pattern              string        `json:"pattern,omitempty"`

	Defs map[string]*Schema `json:"$defs,omitempty"`
}

type SchemaOrBool struct {
	Schema *Schema `json:"schema,omitempty"`
	Bool   *bool   `json:"bool,omitempty"`
}

///////////////////////////////////////////////////////////////////////////////
// Root Schema
///////////////////////////////////////////////////////////////////////////////

func (g *Generator) buildRootSchema(ti *universe.TypeInfo) *Schema {
	return &Schema{
		Schema:               "https://json-schema.org/draft/2020-12/schema",
		ID:                   g.schemaID(ti),
		Title:                ti.Key.TypeName,
		Description:          ti.Comment,
		Type:                 "object",
		Properties:           g.buildStructProperties(ti.Struct, ti),
		Required:             g.buildStructRequired(ti.Struct),
		AdditionalProperties: &SchemaOrBool{Bool: ptr(false)},
	}
}

func (g *Generator) schemaID(ti *universe.TypeInfo) string {
	return ti.Key.PkgPath + "/schemas/" + ti.Key.TypeName + ".schema.json"
}

///////////////////////////////////////////////////////////////////////////////
// Struct handling
///////////////////////////////////////////////////////////////////////////////

func (g *Generator) buildStructProperties(st *ast.StructType, ti *universe.TypeInfo) map[string]*Schema {
	props := map[string]*Schema{}

	for _, field := range st.Fields.List {
		if len(field.Names) == 0 {
			continue
		}

		name := field.Names[0].Name
		jsonName := jsonTagName(field, name)
		if jsonName == "-" {
			continue
		}

		props[jsonName] = g.schemaForExpr(field.Type, ti)
	}

	return props
}

func (g *Generator) buildStructRequired(st *ast.StructType) []string {
	var req []string
	for _, field := range st.Fields.List {
		if len(field.Names) == 0 {
			continue
		}
		if !jsonTagHasOmitEmpty(field) {
			req = append(req, jsonTagName(field, field.Names[0].Name))
		}
	}
	return req
}

///////////////////////////////////////////////////////////////////////////////
// Expression → Schema (core dispatcher)
///////////////////////////////////////////////////////////////////////////////

func (g *Generator) schemaForExpr(expr ast.Expr, ctx *universe.TypeInfo) *Schema {
	switch t := expr.(type) {

	case *ast.Ident:
		return g.schemaForIdent(t, ctx)

	case *ast.SelectorExpr:
		return g.schemaForSelector(t, ctx)

	case *ast.StarExpr:
		return g.schemaForExpr(t.X, ctx)

	case *ast.ArrayType:
		return &Schema{
			Type:  "array",
			Items: g.schemaForExpr(t.Elt, ctx),
		}

	case *ast.MapType:
		value := g.schemaForExpr(t.Value, ctx)
		return &Schema{
			Type: "object",
			AdditionalProperties: &SchemaOrBool{
				Schema: value,
			},
		}

	case *ast.StructType:
		// anonymous embedded struct
		return g.inlineAnonymousStruct(t, ctx)

	default:
		// unknown → free-form object
		return anyObjectSchema()
	}
}

func (g *Generator) schemaForIdent(id *ast.Ident, ctx *universe.TypeInfo) *Schema {
	// primitive
	if prim := primitiveSchema(id.Name); prim != nil {
		return prim
	}

	// same-package named type?
	if ti, ok := g.U.ResolveIdent(ctx.FilePath, ctx.Key.PkgPath, id); ok {
		return &Schema{Ref: "#/$defs/" + universe.DefName(ti.Key)}
	}

	// unknown external primitive-like ident
	return anyObjectSchema()
}

func (g *Generator) schemaForSelector(sel *ast.SelectorExpr, ctx *universe.TypeInfo) *Schema {
	if ti, ok := g.U.ResolveSelector(ctx.FilePath, sel); ok {
		return &Schema{Ref: "#/$defs/" + universe.DefName(ti.Key)}
	}

	// external but not in Universe → treat as plain object
	return anyObjectSchema()
}

func (g *Generator) inlineAnonymousStruct(st *ast.StructType, ctx *universe.TypeInfo) *Schema {
	props := map[string]*Schema{}
	var req []string

	for _, field := range st.Fields.List {
		if len(field.Names) == 0 {
			continue
		}
		name := field.Names[0].Name
		jsonName := jsonTagName(field, name)
		if jsonName == "-" {
			continue
		}

		props[jsonName] = g.schemaForExpr(field.Type, ctx)

		if !jsonTagHasOmitEmpty(field) {
			req = append(req, jsonName)
		}
	}

	return &Schema{
		Type:                 "object",
		Properties:           props,
		Required:             req,
		AdditionalProperties: &SchemaOrBool{Bool: boolPtr(false)},
	}
}

func (g *Generator) collectReachableDefs(root *universe.TypeInfo) map[string]*Schema {
	seen := map[universe.TypeKey]bool{}
	defs := map[string]*Schema{}

	var walk func(ti *universe.TypeInfo)
	walk = func(ti *universe.TypeInfo) {
		if seen[ti.Key] {
			return
		}
		seen[ti.Key] = true

		// walk its struct fields
		for _, field := range ti.Struct.Fields.List {
			g.collectFromExpr(field.Type, ti, walk)
		}

		// add non-root types
		if ti.Key != root.Key {
			defs[universe.DefName(ti.Key)] = g.buildRootSchema(ti)
		}
	}

	walk(root)
	return defs
}

func (g *Generator) collectFromExpr(expr ast.Expr, ctx *universe.TypeInfo, walk func(*universe.TypeInfo)) {
	switch t := expr.(type) {

	case *ast.Ident:
		if ti, ok := g.U.ResolveIdent(ctx.FilePath, ctx.Key.PkgPath, t); ok {
			walk(ti)
		}

	case *ast.SelectorExpr:
		if ti, ok := g.U.ResolveSelector(ctx.FilePath, t); ok {
			walk(ti)
		}

	case *ast.StarExpr:
		g.collectFromExpr(t.X, ctx, walk)

	case *ast.ArrayType:
		g.collectFromExpr(t.Elt, ctx, walk)

	case *ast.MapType:
		g.collectFromExpr(t.Value, ctx, walk)

	case *ast.StructType:
		for _, f := range t.Fields.List {
			g.collectFromExpr(f.Type, ctx, walk)
		}
	}
}

func primitiveSchema(name string) *Schema {
	switch name {
	case "string":
		return &Schema{Type: "string"}
	case "bool":
		return &Schema{Type: "boolean"}
	case "int", "int32", "int64":
		return &Schema{Type: "integer"}
	case "float32", "float64":
		return &Schema{Type: "number"}
	}
	return nil
}

func anyObjectSchema() *Schema {
	return &Schema{
		Type:                 "object",
		AdditionalProperties: &SchemaOrBool{Bool: boolPtr(true)},
	}
}

func boolPtr(v bool) *bool { return &v }
