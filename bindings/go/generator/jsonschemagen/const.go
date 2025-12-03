package jsonschemagen

import (
	"strings"

	"ocm.software/open-component-model/bindings/go/generator/universe"
)

// ApplyConstEnum populates s.OneOf from const values declared for the type.
// Marker-based enums always override const-based enums.
// Additionally, const-level doc comments are copied into the "description" field.
func ApplyConstEnum(s *JSONSchemaDraft202012, consts []*universe.ConstInfo) {
	if s == nil || len(consts) == 0 {
		return
	}

	// Build metadata from consts: literal → (desc, deprecated)
	type constMeta struct {
		desc       string
		deprecated bool
	}

	meta := map[any]constMeta{}
	var order []any

	for _, c := range consts {
		lit, ok := c.Literal()
		if !ok {
			continue
		}

		// const-level markers
		markers := ExtractMarkers(c.Doc, BaseMarker)
		for k, v := range ExtractMarkers(c.Comment, BaseMarker) {
			markers[k] = v
		}

		desc, deprecated := extractConstDescription(c)
		if !deprecated {
			_, deprecated = markers["enum:deprecated"]
		}

		// keep first occurrence order
		if _, exists := meta[lit]; !exists {
			order = append(order, lit)
		}
		meta[lit] = constMeta{
			desc:       desc,
			deprecated: deprecated,
		}
	}

	if len(meta) == 0 {
		return
	}

	// CASE 1: no existing enum/oneOf → build oneOf from consts
	if len(s.OneOf) == 0 && len(s.Enum) == 0 {
		var oneOf []*JSONSchemaDraft202012
		for _, lit := range order {
			m := meta[lit]
			entry := &JSONSchemaDraft202012{
				Const:       lit,
				Description: m.desc,
			}
			if m.deprecated {
				b := true
				entry.Deprecated = &b
			}
			oneOf = append(oneOf, entry)
		}
		s.OneOf = oneOf
		return
	}

	// CASE 2: oneOf already present (e.g. from +ocm:jsonschema-gen:enum=…)
	// → enrich matching entries with description/Deprecated from consts.
	if len(s.OneOf) > 0 {
		for _, entry := range s.OneOf {
			m, ok := meta[entry.Const]
			if !ok {
				continue
			}
			if entry.Description == "" && m.desc != "" {
				entry.Description = m.desc
			}
			if m.deprecated && entry.Deprecated == nil {
				b := true
				entry.Deprecated = &b
			}
		}
	}
}

func extractConstDescription(c *universe.ConstInfo) (string, bool) {
	if c == nil {
		return "", false
	}
	lines, deprecated := collectDoc(c.Doc)
	if len(lines) == 0 {
		lines, deprecated = collectDoc(c.Comment)
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), deprecated
}
