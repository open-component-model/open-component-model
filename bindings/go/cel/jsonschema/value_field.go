package jsonschema

// Field specifies a field name and a reference to a dynamic value.
type Field struct {
	ID   int64
	Name string
	Ref  *DynValue
}
