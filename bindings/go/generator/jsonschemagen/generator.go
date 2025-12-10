package jsonschemagen

import (
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
	schema := g.buildRootSchema(root)

	reachable := g.collectReachableQueue(root)
	defs := map[string]*JSONSchemaDraft202012{}

	for _, ti := range reachable {
		key := universe.Definition(ti.Key)

		// Build full schema, but flatten its defs away
		full := g.buildRootSchema(ti)
		full.Defs = nil // Flatten: no nested defs

		typeMarkers := ExtractMarkerMap(ti.TypeSpec, ti.GenDecl, BaseMarker)
		ApplyNumericMarkers(full, typeMarkers)
		ApplyEnumMarkers(full, typeMarkers)
		ApplyConstEnum(full, ti.Consts)

		defs[key] = full
	}

	schema.Defs = defs

	// TODO (jakobmoellerdev): Currently, all schema IDs are virtual IDs that are not actually
	//   resolvable by URL. This means that we now drop the ID field from the references so that
	//   the definitions act as if they were part of the root schema. This effectively means
	//   it is impossible to deduplicate the schemes in nested types, but always allows correct
	//   referencing from defs.
	for _, def := range schema.Defs {
		def.ID = ""
	}

	return schema
}
