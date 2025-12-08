package jsonschema

import (
	"iter"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// NewSchemaDeclType creates a DeclType wrapping the given santhosh-tekuri/jsonschema Schema.
func NewSchemaDeclType(s *jsonschema.Schema) *DeclType {
	base := NewDeclType(&Schema{Schema: s})
	if base == nil {
		return nil
	}
	if s != nil && s.ID != "" {
		base.Type = base.MaybeAssignTypeName(s.ID)
	}
	return base
}

// Schema wraps a santhosh-tekuri/jsonschema Schema to provide accessors for use in introspection
// and with CEL declaration generation.
type Schema struct {
	Schema *jsonschema.Schema
}

func (s *Schema) Type() string {
	if s.Schema == nil || s.Schema.Types == nil || s.Schema.Types.IsEmpty() {
		return ""
	}
	return s.Schema.Types.ToStrings()[0]
}

func (s *Schema) Items() *Schema {
	if s.Schema == nil || (s.Schema.Items == nil && s.Schema.Items2020 == nil) {
		return nil
	}
	if s.Schema.Items2020 != nil {
		return &Schema{Schema: s.Schema.Items2020}
	}
	switch items := s.Schema.Items.(type) {
	case *jsonschema.Schema:
		return &Schema{Schema: items}
	case []*jsonschema.Schema:
		if len(items) == 0 {
			return nil
		}
		return &Schema{Schema: items[0]}
	default:
		return nil
	}
}

func (s *Schema) Properties() map[string]*Schema {
	if s.Schema == nil || s.Schema.Properties == nil {
		return nil
	}
	res := make(map[string]*Schema, len(s.Schema.Properties))
	for name, prop := range s.Schema.Properties {
		if prop == nil {
			continue
		}
		res[name] = &Schema{Schema: prop}
	}
	return res
}

func (s *Schema) AdditionalPropertiesAsBool() *bool {
	if s.Schema == nil || s.Schema.AdditionalProperties == nil {
		return nil
	}
	if allow, ok := s.Schema.AdditionalProperties.(bool); ok {
		return &allow
	}
	return nil
}

func (s *Schema) AdditionalProperties() *Schema {
	if s.Schema == nil || s.Schema.AdditionalProperties == nil {
		return nil
	}
	if propSchema, ok := s.Schema.AdditionalProperties.(*jsonschema.Schema); ok {
		return &Schema{Schema: propSchema}
	}
	return nil
}

func (s *Schema) Required() []string {
	if s.Schema == nil || s.Schema.Required == nil {
		return nil
	}
	return s.Schema.Required
}

func (s *Schema) Enum() []any {
	if s.Schema == nil || s.Schema.Enum == nil {
		return nil
	}
	return s.Schema.Enum.Values
}

func (s *Schema) MaxItems() *uint64 {
	if s.Schema == nil || s.Schema.MaxItems == nil {
		return nil
	}
	v := safeIntToInt(s.Schema.MaxItems)
	return &v
}

func (s *Schema) MaxProperties() *uint64 {
	if s.Schema == nil || s.Schema.MaxProperties == nil {
		return nil
	}
	v := safeIntToInt(s.Schema.MaxProperties)
	return &v
}

func (s *Schema) MaxLength() *uint64 {
	if s.Schema == nil || s.Schema.MaxLength == nil {
		return nil
	}
	v := safeIntToInt(s.Schema.MaxLength)
	return &v
}

func (s *Schema) Default() any {
	if s.Schema == nil || s.Schema.Default == nil {
		return nil
	}
	return s.Schema.Default
}

func (s *Schema) Format() string {
	if s.Schema == nil || s.Schema.Format == nil {
		return ""
	}
	return s.Schema.Format.Name
}

func (s *Schema) Ref() *Schema {
	if s.Schema == nil || s.Schema.Ref == nil {
		return nil
	}
	return &Schema{Schema: s.Schema.Ref}
}

func (s *Schema) Const() any {
	if s.Schema == nil || s.Schema.Const == nil {
		return nil
	}
	return *s.Schema.Const
}

func (s *Schema) OneOf() iter.Seq2[int, *Schema] {
	if s.Schema == nil || s.Schema.OneOf == nil {
		return func(yield func(int, *Schema) bool) {}
	}
	return func(yield func(int, *Schema) bool) {
		for i, sch := range s.Schema.OneOf {
			if sch == nil {
				continue
			}
			if !yield(i, &Schema{Schema: sch}) {
				return
			}
		}
	}
}

func safeIntToInt(u *int) uint64 {
	if u == nil {
		return 0
	}
	if *u > 0 {
		return uint64(*u)
	}
	return 0
}
