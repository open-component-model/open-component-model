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
	defs := g.collectReachableDefs(root)

	if len(defs) > 0 {
		schema.Defs = defs
	}

	return schema
}
