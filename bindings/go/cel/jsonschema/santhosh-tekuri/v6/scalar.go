package jsonschema

import "github.com/santhosh-tekuri/jsonschema/v6"

// JSON Schema primitive type names.
// Use TypeForSchema to create an actual schema type.
var (
	BooleanType = "boolean"
	IntegerType = "integer"
	NumType     = "number"
	StringType  = "string"
	ArrayType   = "array"
	ObjectType  = "object"
	NullType    = "null"
)

func TypeForSchema(typ string) *jsonschema.Types {
	var t jsonschema.Types
	t.Add(typ)
	return &t
}
