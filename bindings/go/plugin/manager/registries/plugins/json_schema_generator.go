package plugins

import (
	"fmt"
	"reflect"

	"github.com/invopop/jsonschema"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// GenerateJSONSchemaForType takes a Type and uses reflection to generate a JSON Schema representation for it.
// It will also use the correct type representation as we don't marshal the type in object format.
func GenerateJSONSchemaForType(obj runtime.Typed) ([]byte, error) {
	if obj == nil {
		return nil, fmt.Errorf("cannot generate JSON schema for nil object")
	}

	switch obj.(type) {
	case *runtime.Unstructured, *runtime.Raw:
		return nil, fmt.Errorf("unstructured or raw object type is unsupported")
	}

	r := &jsonschema.Reflector{
		Mapper: func(i reflect.Type) *jsonschema.Schema {
			if i == reflect.TypeOf(runtime.Type{}) {
				return &jsonschema.Schema{
					Type:    "string",
					Pattern: `^([a-zA-Z0-9][a-zA-Z0-9.]*)(?:/(v[0-9]+(?:alpha[0-9]+|beta[0-9]+)?))?`,
				}
			}
			return nil
		},
	}

	schema, err := r.ReflectFromType(reflect.TypeOf(obj)).MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("failed to create json schema for object: %w", err)
	}

	return schema, nil
}
