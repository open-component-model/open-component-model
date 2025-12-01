package jsonschema

import (
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// DeclField describes the name, ordinal, and optionality of a field declaration within a type.
type DeclField struct {
	Name         string
	Type         *DeclType
	Required     bool
	enumValues   []any
	defaultValue any
}

// DefaultValue returns the zero value associated with the field.
func (f *DeclField) DefaultValue() ref.Val {
	if f.defaultValue != nil {
		return types.DefaultTypeAdapter.NativeToValue(f.defaultValue)
	}
	return f.Type.DefaultValue()
}

// TypeName returns the string type name of the field.
func (f *DeclField) TypeName() string {
	return f.Type.TypeName()
}

// EnumValues returns the set of values that this field may take.
func (f *DeclField) EnumValues() []ref.Val {
	if f.enumValues == nil || len(f.enumValues) == 0 {
		return []ref.Val{}
	}
	ev := make([]ref.Val, len(f.enumValues))
	for i, e := range f.enumValues {
		ev[i] = types.DefaultTypeAdapter.NativeToValue(e)
	}
	return ev
}
