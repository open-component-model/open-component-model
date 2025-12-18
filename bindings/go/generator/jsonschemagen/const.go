package jsonschemagen

import (
	"go/constant"
	"strings"

	"ocm.software/open-component-model/bindings/go/generator/universe"
)

// ApplyConstEnum populates or enriches s.OneOf from enum values declared
// as Go consts for the type.
//
// Rules:
//   - Marker-based enums always win structurally
//   - Const-based enums provide descriptions + deprecated flags
//   - If no enum exists yet, const enums create it
func ApplyConstEnum(s *JSONSchemaDraft202012, enums []*universe.Const) {
	if s == nil || len(enums) == 0 {
		return
	}

	// Build metadata from consts: literal → (desc, deprecated)
	type constMeta struct {
		desc       string
		deprecated bool
	}

	meta := map[any]constMeta{}
	var order []any

	for _, ev := range enums {
		lit, ok := constLiteral(ev)
		if !ok {
			continue
		}

		// collect markers
		markers := ExtractMarkers(ev.Doc, BaseMarker)
		for k, v := range ExtractMarkers(ev.Comment, BaseMarker) {
			markers[k] = v
		}

		desc, deprecated := extractEnumDescription(ev)
		if !deprecated {
			_, deprecated = markers["enum:deprecated"]
		}

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

	// CASE 1: no enum yet → create oneOf from const enums
	if len(s.OneOf) == 0 && len(s.Enum) == 0 {
		var oneOf []*JSONSchemaDraft202012
		for _, lit := range order {
			m := meta[lit]
			entry := &JSONSchemaDraft202012{
				Const:       lit,
				Description: m.desc,
			}
			if m.deprecated {
				entry.Deprecated = Ptr(true)
			}
			oneOf = append(oneOf, entry)
		}
		s.OneOf = oneOf
		return
	}

	// CASE 2: enum already exists → enrich entries
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
				entry.Deprecated = Ptr(true)
			}
		}
	}
}

func extractEnumDescription(ev *universe.Const) (string, bool) {
	lines, deprecated := collectDoc(ev.Doc)
	if len(lines) == 0 {
		lines, deprecated = collectDoc(ev.Comment)
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), deprecated
}

func constLiteral(c *universe.Const) (any, bool) {
	if c.Obj == nil {
		return nil, false
	}

	v := c.Obj.Val()

	switch v.Kind() {
	case constant.String:
		return constant.StringVal(v), true
	case constant.Int:
		i, _ := constant.Int64Val(v)
		return i, true
	case constant.Bool:
		return constant.BoolVal(v), true
	default:
		return nil, false
	}
}
