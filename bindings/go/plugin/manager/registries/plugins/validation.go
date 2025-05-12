package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// ValidatePlugin will take a runtime Type and validate it against the given JSON Schema.
func ValidatePlugin(typ runtime.Typed, jsonSchema []byte) (bool, error) {
	c := jsonschema.NewCompiler()
	unmarshaler, err := jsonschema.UnmarshalJSON(bytes.NewReader(jsonSchema))
	if err != nil {
		return false, err
	}

	var v any
	if err := json.Unmarshal(jsonSchema, &v); err != nil {
		return false, err
	}

	if err := c.AddResource("schema.json", unmarshaler); err != nil {
		return false, fmt.Errorf("failed to add schema.json: %w", err)
	}
	sch, err := c.Compile("schema.json")
	if err != nil {
		return false, fmt.Errorf("failed to compile schema.json: %w", err)
	}

	// need to marshal the interface into a JSON format.
	content, err := json.Marshal(typ)
	if err != nil {
		return false, fmt.Errorf("failed to marshal type: %w", err)
	}
	// once marshalled, we create a map[string]any representation of the marshaled content.
	unmarshalledType, err := jsonschema.UnmarshalJSON(bytes.NewReader(content))
	if err != nil {
		return false, fmt.Errorf("failed to unmarshal : %w", err)
	}

	if _, ok := unmarshalledType.(string); ok {
		return false, nil
	}

	// finally, validate map[string]any against the loaded schema
	if err := sch.Validate(unmarshalledType); err != nil {
		var typRaw bytes.Buffer
		err = errors.Join(err, json.Indent(&typRaw, content, "", "  "))
		var schemaRaw bytes.Buffer
		err = errors.Join(err, json.Indent(&schemaRaw, jsonSchema, "", "  "))
		return false, fmt.Errorf("failed to validate schema for\n%s\n---SCHEMA---\n%s\n: %w", typRaw.String(), schemaRaw.String(), err)
	}

	return true, nil
}
