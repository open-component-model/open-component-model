package jsonschema

import (
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// InferFromGoValue converts a generic Go runtime value into a minimal JSON
// Schema compatible with the github.com/santhosh-tekuri/jsonschema/v6 package.
//
// The function accepts values that correspond to JSON types:
//   - bool                  → { type: "boolean", const: <value> }
//   - int64, uint64         → { type: "integer", const: <value> }
//   - float64               → { type: "number",  const: <value> }
//   - string                → { type: "string",  const: <value> }
//   - []interface{}         → { type: "array",   items: <inferred from first element> }
//   - map[string]interface{} → { type: "object", properties: <inferred per key> }
//
// Primitive values produce schemas that constrain the type and require the
// exact value via a const keyword.
//
// Arrays produce an array schema. If the array is non-empty, the items schema
// is inferred from the first element only; mixed or heterogeneous arrays are
// not analyzed. Empty arrays produce an unconstrained array type.
//
// Objects produce an object schema whose properties map to recursively inferred
// schemas. Required properties are not inferred.
//
// Unsupported values return an error.
//
// This inference mechanism is intentionally minimal and is intended for simple,
// predictable schema construction from JSON-like Go values, not for structural
// merging or advanced constraint generation.
func InferFromGoValue(goRuntimeVal interface{}) (*jsonschema.Schema, error) {
	switch typed := goRuntimeVal.(type) {
	case bool:
		return &jsonschema.Schema{
			Types: typeForSchema("boolean"),
			Const: &goRuntimeVal,
		}, nil
	case int64:
		return &jsonschema.Schema{
			Types: typeForSchema("integer"),
			Const: &goRuntimeVal,
		}, nil
	case uint64:
		return &jsonschema.Schema{
			Types: typeForSchema("integer"),
			Const: &goRuntimeVal,
		}, nil
	case float64:
		return &jsonschema.Schema{
			Types: typeForSchema("number"),
			Const: &goRuntimeVal,
		}, nil
	case string:
		return &jsonschema.Schema{
			Types: typeForSchema("string"),
			Const: &goRuntimeVal,
		}, nil
	case []interface{}:
		return inferArraySchema(typed)
	case map[string]interface{}:
		return inferObjectSchema(typed)
	default:
		return nil, fmt.Errorf("unsupported type: %T", goRuntimeVal)
	}
}

func inferArraySchema(arr []interface{}) (*jsonschema.Schema, error) {
	schema := &jsonschema.Schema{
		Types: typeForSchema("array"),
	}

	if len(arr) > 0 {
		itemSchema, err := InferFromGoValue(arr[0])
		if err != nil {
			return nil, fmt.Errorf("failed to infer schema for array item: %w", err)
		}
		schema.Items2020 = itemSchema
	}

	return schema, nil
}

func inferObjectSchema(obj map[string]interface{}) (*jsonschema.Schema, error) {
	schema := &jsonschema.Schema{
		Types:      typeForSchema("object"),
		Properties: make(map[string]*jsonschema.Schema),
	}

	for key, value := range obj {
		propSchema, err := InferFromGoValue(value)
		if err != nil {
			return nil, fmt.Errorf("failed to infer schema for property %s: %w", key, err)
		}
		schema.Properties[key] = propSchema
	}

	return schema, nil
}

func typeForSchema(typ string) *jsonschema.Types {
	var t jsonschema.Types
	t.Add(typ)
	return &t
}
