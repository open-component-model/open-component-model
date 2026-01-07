package jsonschemagen

import (
	"maps"

	"ocm.software/open-component-model/bindings/go/generator/universe"
)

type Generator struct {
	U *universe.Universe
}

func New(u *universe.Universe) *Generator {
	return &Generator{U: u}
}

// GenerateJSONSchemaDraft202012 builds a JSON JSONSchemaDraft202012 for a root type.
func (g *Generator) GenerateJSONSchemaDraft202012(root *universe.TypeInfo) *JSONSchemaDraft202012 {
	return (&generation{Generator: g}).runForRoot(root)
}

// generation is the generation context for a schema Generator run.
type generation struct {
	*Generator
	// collected external referenced schemas that should be merged into the defs section at the end of the generation
	external map[string]*JSONSchemaDraft202012
}

// runForRoot starts generation for a JSONSchemaDraft202012 for the given root type info.
func (g *generation) runForRoot(root *universe.TypeInfo) *JSONSchemaDraft202012 {
	schema := g.buildRootSchema(root)

	reachable := g.collectReachableQueue(root)
	defs := map[string]*JSONSchemaDraft202012{}

	for _, ti := range reachable {
		key := universe.Definition(ti.Key)

		full := g.buildRootSchema(ti)
		full.Defs = nil

		typeMarkers := ExtractMarkerMap(ti.TypeSpec, ti.GenDecl, BaseMarker)
		ApplyNumericMarkers(full, typeMarkers)
		ApplyEnumMarkers(full, typeMarkers)
		ApplyConstEnum(full, ti.Consts)

		defs[key] = full
	}

	for _, def := range defs {
		def.ID = ""
	}

	// merge externals exactly once
	maps.Copy(defs, g.external)

	schema.Defs = defs
	return schema
}
