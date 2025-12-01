package jsonschema

import (
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// NewObjectValue creates a struct value with a schema type and returns the empty ObjectValue.
func NewObjectValue(sType *DeclType) *ObjectValue {
	return &ObjectValue{
		structValue: newStructValue(),
		objectType:  sType,
	}
}

// ObjectValue is a struct with a custom schema type which indicates the fields and types
// associated with the structure.
type ObjectValue struct {
	*structValue
	objectType *DeclType
}

// ConvertToType is an implementation of the CEL ref.Val interface method.
func (o *ObjectValue) ConvertToType(t ref.Type) ref.Val {
	if t == types.TypeType {
		return types.NewObjectTypeValue(o.objectType.TypeName())
	}
	if t.TypeName() == o.objectType.TypeName() {
		return o
	}
	return types.NewErr("type conversion error from '%s' to '%s'", o.Type(), t)
}

// Equal returns true if the two object types are equal and their field values are equal.
func (o *ObjectValue) Equal(other ref.Val) ref.Val {
	// Preserve CEL's homogeneous equality semantics.
	if o.objectType.TypeName() != other.Type().TypeName() {
		return types.MaybeNoSuchOverloadErr(other)
	}
	o2 := other.(traits.Indexer)
	for name := range o.objectType.Fields {
		k := types.String(name)
		v := o.Get(k)
		ov := o2.Get(k)
		vEq := v.Equal(ov)
		if vEq != types.True {
			return vEq
		}
	}
	return types.True
}

// Get returns the value of the specified field.
//
// If the field is set, its value is returned. If the field is not set, the default value for the
// field is returned thus allowing for safe-traversal and preserving proto-like field traversal
// semantics for Open API Schema backed types.
func (o *ObjectValue) Get(name ref.Val) ref.Val {
	n, ok := name.(types.String)
	if !ok {
		return types.MaybeNoSuchOverloadErr(n)
	}
	nameStr := string(n)
	field, found := o.fieldMap[nameStr]
	if found {
		return field.Ref.ExprValue()
	}
	fieldDef, found := o.objectType.Fields[nameStr]
	if !found {
		return types.NewErr("no such field: %s", nameStr)
	}
	defValue := fieldDef.DefaultValue()
	if defValue != nil {
		return defValue
	}
	return types.NewErr("no default for type: %s", fieldDef.TypeName())
}

// Type returns the CEL type value of the object.
func (o *ObjectValue) Type() ref.Type {
	return o.objectType
}

// Value returns the Go-native representation of the object.
func (o *ObjectValue) Value() interface{} {
	return o
}
