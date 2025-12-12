package jsonschemagen

import (
	"strings"

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

	defs := map[string]*JSONSchemaDraft202012{}
	protected := map[string]struct{}{}

	for _, ti := range g.collectReachableQueue(root) {
		key := universe.Definition(ti.Key)

		full := g.buildRootSchema(ti)

		// Flatten all externally identified schemas into root defs
		for _, subschema := range full.Properties {
			g.flattenExternalSchemas(subschema, defs, protected)
		}

		full.Defs = nil

		markers := ExtractMarkerMap(ti.TypeSpec, ti.GenDecl, BaseMarker)
		ApplyNumericMarkers(full, markers)
		ApplyEnumMarkers(full, markers)
		ApplyConstEnum(full, ti.Consts)

		defs[key] = full
	}

	// Drop non-protected IDs (virtual IDs are not resolvable)
	for k, def := range defs {
		if _, keep := protected[k]; !keep {
			def.ID = ""
		}
	}

	schema.Defs = defs
	return schema
}

func (g *Generator) flattenExternalSchemas(
	s *JSONSchemaDraft202012,
	defs map[string]*JSONSchemaDraft202012,
	protected map[string]struct{},
) {
	for _, p := range s.Properties {
		g.flattenExternalSchemas(p, defs, protected)
	}
	if s.ID == "" {
		return
	}

	key := strings.ReplaceAll(s.ID, "/", ".")
	cp := *s

	defs[key] = &cp
	protected[key] = struct{}{}

	*s = JSONSchemaDraft202012{
		Ref: "#/$defs/" + key,
	}
}
