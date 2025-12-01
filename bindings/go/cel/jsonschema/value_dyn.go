package jsonschema

import (
	"reflect"
	"time"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// NewEmptyDynValue returns the zero-valued DynValue.
func NewEmptyDynValue() *DynValue {
	// note: 0 is not a valid parse node identifier.
	dv, _ := NewDynValue(0, nil)
	return dv
}

// NewDynValue returns a DynValue that corresponds to a parse node id and value.
func NewDynValue(id int64, val interface{}) (*DynValue, error) {
	dv := &DynValue{ID: id}
	err := dv.SetValue(val)
	return dv, err
}

// DynValue is a dynamically typed value used to describe unstructured content.
// Whether the value has the desired type is determined by where it is used within the Instance or
// Template, and whether there are schemas which might enforce a more rigid type definition.
type DynValue struct {
	ID          int64
	EncodeStyle EncodeStyle
	value       interface{}
	exprValue   ref.Val
	declType    *DeclType
}

// DeclType returns the policy model type of the dyn value.
func (dv *DynValue) DeclType() *DeclType {
	return dv.declType
}

// ConvertToNative is an implementation of the CEL ref.Val method used to adapt between CEL types
// and Go-native types.
//
// The default behavior of this method is to first convert to a CEL type which has a well-defined
// set of conversion behaviors and proxy to the CEL ConvertToNative method for the type.
func (dv *DynValue) ConvertToNative(typeDesc reflect.Type) (interface{}, error) {
	ev := dv.ExprValue()
	if types.IsError(ev) {
		return nil, ev.(*types.Err)
	}
	return ev.ConvertToNative(typeDesc)
}

// Equal returns whether the dyn value is equal to a given CEL value.
func (dv *DynValue) Equal(other ref.Val) ref.Val {
	dvType := dv.Type()
	otherType := other.Type()
	// Preserve CEL's homogeneous equality constraint.
	if dvType.TypeName() != otherType.TypeName() {
		return types.MaybeNoSuchOverloadErr(other)
	}
	switch v := dv.value.(type) {
	case ref.Val:
		return v.Equal(other)
	case PlainTextValue:
		return celBool(string(v) == other.Value().(string))
	case *MultilineStringValue:
		return celBool(v.Value == other.Value().(string))
	case time.Duration:
		otherDuration := other.Value().(time.Duration)
		return celBool(v == otherDuration)
	case time.Time:
		otherTimestamp := other.Value().(time.Time)
		return celBool(v.Equal(otherTimestamp))
	default:
		return celBool(reflect.DeepEqual(v, other.Value()))
	}
}

// ExprValue converts the DynValue into a CEL value.
func (dv *DynValue) ExprValue() ref.Val {
	return dv.exprValue
}

// Value returns the underlying value held by this reference.
func (dv *DynValue) Value() interface{} {
	return dv.value
}

// SetValue updates the underlying value held by this reference.
func (dv *DynValue) SetValue(value interface{}) error {
	dv.value = value
	var err error
	dv.exprValue, dv.declType, err = exprValue(value)
	return err
}

// Type returns the CEL type for the given value.
func (dv *DynValue) Type() ref.Type {
	return dv.ExprValue().Type()
}
