package runtime

// JSONSchemaIntrospectable defines a type that can provide its JSON Schema
// representation as a raw byte slice. Implementers typically generate and embed
// the schema alongside the type and return it from JSONSchema().
type JSONSchemaIntrospectable interface {
	// JSONSchema returns the JSON Schema for the implementing type.
	// The returned schema must be valid against a valid JSON Schema specification.
	// If implemented, MUST return a valid JSON Schema.
	JSONSchema() []byte
}
