package jsonschema

import (
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

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
