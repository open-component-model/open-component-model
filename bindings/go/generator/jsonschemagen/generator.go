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

	reachable := g.collectReachableQueue(root)
	defs := map[string]*JSONSchemaDraft202012{}
	protectedFromIDRemoval := map[string]struct{}{}

	for _, ti := range reachable {
		key := universe.Definition(ti.Key)

		// Build full schema, but flatten its defs away
		full := g.buildRootSchema(ti)

		// Collect external properties into defs.
		// any property with an ID is considered external
		// we replace it with a $ref to the defs section
		// of the root schema.
		// This effectively flattens all properties with an ID into the root defs.
		// That will deduplicate types that are used in multiple places.
		var collectExternalProps func(s *JSONSchemaDraft202012)
		collectExternalProps = func(s *JSONSchemaDraft202012) {
			if len(s.Properties) > 0 {
				for _, p := range s.Properties {
					collectExternalProps(p)
				}
			}
			if s.ID == "" {
				return
			}
			normalized := strings.ReplaceAll(s.ID, "/", ".")
			cp := *s
			defs[normalized] = &cp
			protectedFromIDRemoval[normalized] = struct{}{}
			*s = JSONSchemaDraft202012{Ref: "#/$defs/" + normalized}
		}
		for _, prop := range full.Properties {
			collectExternalProps(prop)
		}

		full.Defs = nil

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
	for key, def := range schema.Defs {
		if _, ok := protectedFromIDRemoval[key]; ok {
			continue
		}
		def.ID = ""
	}

	return schema
}
