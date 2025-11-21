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

// Generate builds a JSON Schema for a root type.
func (g *Generator) Generate(root *universe.TypeInfo) *Schema {
	schema := g.buildRootSchema(root)

	reachable := g.collectReachableQueue(root)
	defs := map[string]*Schema{}

	for _, ti := range reachable {
		key := universe.Definition(ti.Key)

		// Build full schema, but flatten its defs away
		full := g.buildRootSchema(ti)
		full.Defs = nil // Flatten: no nested defs

		defs[key] = full
	}

	schema.Defs = defs
	return schema
}
