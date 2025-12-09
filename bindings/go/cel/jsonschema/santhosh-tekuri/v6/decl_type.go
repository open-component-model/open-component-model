package jsonschema

import (
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"

	"ocm.software/open-component-model/bindings/go/cel/jsonschema"
	"ocm.software/open-component-model/bindings/go/cel/jsonschema/decl"
)

// DeclType represents a JSON Schema Declaration Type backed by an actual Schema.
type DeclType struct {
	*decl.Type
	*Schema
}

// declTypeForSchema creates a DeclType from the provided decl.Type and Schema.
func declTypeForSchema(t *decl.Type, schema *Schema) *DeclType {
	return &DeclType{
		Type:   t,
		Schema: schema,
	}
}

// NewDeclType converts a structural Schema to a CEL declaration, or returns nil if the
// structural schema cannot be exposed in CEL expressions. Note that this conversion
// only supports an opinionated subset of JSON Schema features.
func NewDeclType(s *Schema) *DeclType {
	if s == nil {
		return nil
	}

	switch s.Type() {
	case "array":
		if s.Items() == nil {
			// JSON Schema default: "items": {}
			return declTypeForSchema(decl.NewListType(decl.DynType, decl.NoMaxLength), s)
		}
		itemsType := NewDeclType(s.Items())
		if itemsType == nil {
			// Fallback: treat unknown array items as dyn
			itemsType = declTypeForSchema(decl.DynType, s.Items())
		}

		var maxItems uint64
		if s.MaxItems() != nil {
			maxItems = *s.MaxItems()
		} else {
			maxItems = estimateMaxArrayItemsFromMinSize(itemsType.MinSerializedSize)
		}

		return declTypeForSchema(
			decl.NewListType(itemsType.Type, maxItems),
			s,
		)

	case "object":
		// If additionalProperties is itself a schema → treat as map<string, X>
		if s.AdditionalProperties() != nil {
			if propsType := NewDeclType(s.AdditionalProperties()); propsType != nil {
				var maxProperties uint64
				if s.MaxProperties() != nil {
					maxProperties = *s.MaxProperties()
				} else {
					maxProperties = estimateMaxAdditionalPropertiesFromMinSize(propsType.MinSerializedSize)
				}
				return declTypeForSchema(
					decl.NewMapType(decl.StringType, propsType.Type, maxProperties),
					s,
				)
			}
			// Fallback: additionalProperties = unknown → Dyn
			// this allows *any* key with dyn value
			return declTypeForSchema(decl.NewMapType(decl.StringType, decl.DynType, decl.NoMaxLength), s)
		}

		// object with named properties
		fields := make(map[string]*decl.Field, len(s.Properties()))

		required := map[string]bool{}
		if s.Required() != nil {
			for _, f := range s.Required() {
				required[f] = true
			}
		}

		// {} is at least 2 bytes
		minSerializedSize := uint64(2)

		for name, prop := range s.Properties() {
			// Extract literal values (enum, const, oneOf-const, ref)
			// NEW: these values will restrict assignment in CEL (literal-only type)
			enumValues := collectEnumAndConstValues(prop)

			// Resolve type (including ref fallback)
			fieldType := NewDeclType(prop)
			if fieldType == nil && prop.Ref() != nil {
				fieldType = NewDeclType(prop.Ref())
			}
			if fieldType == nil {
				continue
			}

			if propName, ok := jsonschema.Escape(name); ok {
				// NEW: enumValues is now passed through so CEL restricts the field
				fields[propName] = decl.NewField(
					propName,
					fieldType.Type,
					required[name],
					enumValues,
					prop.Default(),
				)
			}

			// Required field with no default contributes to minSerializedSize
			if required[name] && prop.Default() == nil {
				// colon, quotes, comma, etc. approx +4
				minSerializedSize += uint64(len(name)) + fieldType.MinSerializedSize + 4
			}
		}

		if len(fields) == 0 {
			// treat as open map , CEL will allow any value with dyn type
			return declTypeForSchema(
				decl.NewMapType(decl.StringType, decl.DynType, decl.NoMaxLength), s,
			)
		}
		id := "object"
		if s.Schema.ID != "" {
			// if we have a unique schema ID, use that
			id = s.Schema.ID
		}
		objType := decl.NewObjectType(id, fields)
		objType.MinSerializedSize = minSerializedSize
		base := declTypeForSchema(objType, s)
		if anyAdditionalProperties := base.AdditionalPropertiesAsBool(); anyAdditionalProperties != nil {
			// if additionalProperties is explicitly allowed/denied, reflect that in the type
			base.SetAdditionalPropertiesMetadata(*anyAdditionalProperties)
			base.MaxElements = decl.NoMaxLength
		}
		return base
	case "string":
		switch s.Format() {
		case "byte":
			t := decl.NewSimpleTypeWithMinSize("bytes", cel.BytesType, types.Bytes([]byte{}), decl.MinStringSize)
			if s.MaxLength() != nil {
				t.MaxElements = *s.MaxLength()
			} else {
				t.MaxElements = estimateMaxStringLengthPerRequest(s)
			}
			return declTypeForSchema(t, s)

		case "duration":
			t := decl.NewSimpleTypeWithMinSize("duration", cel.DurationType, types.Duration{Duration: time.Duration(0)}, uint64(decl.MinDurationSizeJSON))
			t.MaxElements = estimateMaxStringLengthPerRequest(s)
			return declTypeForSchema(t, s)

		case "date":
			t := decl.NewSimpleTypeWithMinSize("timestamp", cel.TimestampType, types.Timestamp{Time: time.Time{}}, uint64(decl.JSONDateSize))
			t.MaxElements = estimateMaxStringLengthPerRequest(s)
			return declTypeForSchema(t, s)

		case "date-time":
			t := decl.NewSimpleTypeWithMinSize("timestamp", cel.TimestampType, types.Timestamp{Time: time.Time{}}, uint64(decl.MinDatetimeSizeJSON))
			t.MaxElements = estimateMaxStringLengthPerRequest(s)
			return declTypeForSchema(t, s)
		}

		str := decl.NewSimpleTypeWithMinSize("string", cel.StringType, types.String(""), decl.MinStringSize)

		switch {
		case s.MaxLength() != nil:
			str.MaxElements = *s.MaxLength()
		case len(s.Enum()) > 0:
			str.MaxElements = estimateMaxStringEnumLength(s)
		default:
			str.MaxElements = estimateMaxStringLengthPerRequest(s)
		}

		return declTypeForSchema(str, s)

	case "boolean":
		return declTypeForSchema(decl.BoolType, s)

	case "number":
		return declTypeForSchema(decl.DoubleType, s)

	case "integer":
		return declTypeForSchema(decl.IntType, s)
	}

	// Ref-only schemas
	if s.Ref() != nil {
		return NewDeclType(s.Ref())
	}

	return nil
}

// estimateMaxStringLengthPerRequest estimates the maximum string length (in characters)
// of a string compatible with the format requirements in the provided schema.
// must only be called on schemas of type "string" or x-kubernetes-int-or-string: true
func estimateMaxStringLengthPerRequest(s *Schema) uint64 {
	switch s.Format() {
	case "duration":
		return decl.MaxDurationSizeJSON
	case "date":
		return decl.JSONDateSize
	case "date-time":
		return decl.MaxDatetimeSizeJSON
	default:
		// subtract 2 to account for ""
		return decl.MaxRequestSizeBytes - 2
	}
}

// estimateMaxStringLengthPerRequest estimates the maximum string length (in characters)
// that has a set of enum values.
// The result of the estimation is the length of the longest possible value.
func estimateMaxStringEnumLength(s *Schema) uint64 {
	var maxLength uint64
	for _, v := range s.Enum() {
		if s, ok := v.(string); ok && uint64(len(s)) > maxLength {
			maxLength = uint64(len(s))
		}
	}
	return maxLength
}

// estimateMaxArrayItemsPerRequest estimates the maximum number of array items with
// the provided minimum serialized size that can fit into a single request.
func estimateMaxArrayItemsFromMinSize(minSize uint64) uint64 {
	// subtract 2 to account for [ and ]
	return (decl.MaxRequestSizeBytes - 2) / (minSize + 1)
}

// estimateMaxAdditionalPropertiesPerRequest estimates the maximum number of additional properties
// with the provided minimum serialized size that can fit into a single request.
func estimateMaxAdditionalPropertiesFromMinSize(minSize uint64) uint64 {
	// 2 bytes for key + "" + colon + comma + smallest possible value, realistically the actual keys
	// will all vary in length
	keyValuePairSize := minSize + 6
	// subtract 2 to account for { and }
	return (decl.MaxRequestSizeBytes - 2) / keyValuePairSize
}

// collectEnumAndConstValues extracts a deduplicated list of literal values
// from enum[], const, and from oneOf branches where a branch defines a const.
//
// Rules:
// 1. If schema.Const is non-nil -> return only that single value.
// 2. enum[] always contributes literal values.
// 3. oneOf[n].Const contributes only that literal.
// 4. If oneOf[n] contains no const, ignore it – it may define a type but not literals.
// 5. If schema has no values but the $ref does, return those.
// 6. Values are deduplicated while preserving original order.
func collectEnumAndConstValues(s *Schema) []interface{} {
	if s == nil {
		return nil
	}

	// Rule 1: const overrides everything
	if c := s.Const(); c != nil {
		return []interface{}{c}
	}

	seen := map[any]struct{}{}
	var out []interface{}

	add := func(v any) {
		// maps can’t use slices, maps, etc. as keys – JSON Schema const/enum
		// generally uses scalar types (string, number, bool, null).
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}

	// Rule 2: enum[]
	if s.Enum() != nil {
		for _, v := range s.Enum() {
			add(v)
		}
	}

	// Rule 3: oneOf const branches
	if s.OneOf() != nil {
		for _, br := range s.OneOf() {
			if br == nil {
				continue
			}
			if c := br.Const(); c != nil {
				add(c)
			}
		}
	}

	// Rule 5: $ref fallback (only if we found nothing locally)
	if len(out) == 0 && s.Ref() != nil {
		return collectEnumAndConstValues(s.Ref())
	}

	return out
}
