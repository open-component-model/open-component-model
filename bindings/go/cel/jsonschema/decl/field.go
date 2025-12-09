package decl

import (
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

func NewField(name string, declType *Type, required bool, enumValues []interface{}, defaultValue interface{}) *Field {
	return &Field{
		Name:         name,
		Type:         declType,
		Required:     required,
		enumValues:   enumValues,
		defaultValue: defaultValue,
	}
}

// Field describes the name, ordinal, and optionality of a field declaration within a type.
type Field struct {
	Name         string
	Type         *Type
	Required     bool
	enumValues   []any
	defaultValue any
}

// DefaultValue returns the zero value associated with the field.
func (f *Field) DefaultValue() ref.Val {
	if f.defaultValue != nil {
		return types.DefaultTypeAdapter.NativeToValue(f.defaultValue)
	}
	return f.Type.DefaultValue()
}

// TypeName returns the string type name of the field.
func (f *Field) TypeName() string {
	return f.Type.TypeName()
}

// EnumValues returns the set of values that this field may take.
func (f *Field) EnumValues() []ref.Val {
	if len(f.enumValues) == 0 {
		return []ref.Val{}
	}
	ev := make([]ref.Val, len(f.enumValues))
	for i, e := range f.enumValues {
		ev[i] = types.DefaultTypeAdapter.NativeToValue(e)
	}
	return ev
}
