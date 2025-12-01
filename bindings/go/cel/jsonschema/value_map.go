package jsonschema

import (
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// NewMapValue returns an empty MapValue.
func NewMapValue() *MapValue {
	return &MapValue{
		structValue: newStructValue(),
	}
}

// MapValue declares an object with a set of named fields whose values are dynamically typed.
type MapValue struct {
	*structValue
}

// ConvertToObject produces an ObjectValue from the MapValue with the associated schema type.
//
// The conversion is shallow and the memory shared between the Object and Map as all references
// to the map are expected to be replaced with the Object reference.
func (m *MapValue) ConvertToObject(declType *DeclType) *ObjectValue {
	return &ObjectValue{
		structValue: m.structValue,
		objectType:  declType,
	}
}

// Contains returns whether the given key is contained in the MapValue.
func (m *MapValue) Contains(key ref.Val) ref.Val {
	v, found := m.Find(key)
	if v != nil && types.IsUnknownOrError(v) {
		return v
	}
	return celBool(found)
}

// ConvertToType converts the MapValue to another CEL type, if possible.
func (m *MapValue) ConvertToType(t ref.Type) ref.Val {
	switch t {
	case types.MapType:
		return m
	case types.TypeType:
		return types.MapType
	}
	return types.NewErr("type conversion error from '%s' to '%s'", m.Type(), t)
}

// Equal returns true if the maps are of the same size, have the same keys, and the key-values
// from each map are equal.
func (m *MapValue) Equal(other ref.Val) ref.Val {
	oMap, isMap := other.(traits.Mapper)
	if !isMap {
		return types.MaybeNoSuchOverloadErr(other)
	}
	if m.Size() != oMap.Size() {
		return types.False
	}
	for name, field := range m.fieldMap {
		k := types.String(name)
		ov, found := oMap.Find(k)
		if !found {
			return types.False
		}
		v := field.Ref.ExprValue()
		vEq := v.Equal(ov)
		if vEq != types.True {
			return vEq
		}
	}
	return types.True
}

// Find returns the value for the key in the map, if found.
func (m *MapValue) Find(name ref.Val) (ref.Val, bool) {
	// Currently only maps with string keys are supported as this is best aligned with JSON,
	// and also much simpler to support.
	n, ok := name.(types.String)
	if !ok {
		return types.MaybeNoSuchOverloadErr(n), true
	}
	nameStr := string(n)
	field, found := m.fieldMap[nameStr]
	if found {
		return field.Ref.ExprValue(), true
	}
	return nil, false
}

// Get returns the value for the key in the map, or error if not found.
func (m *MapValue) Get(key ref.Val) ref.Val {
	v, found := m.Find(key)
	if found {
		return v
	}
	return types.ValOrErr(key, "no such key: %v", key)
}

// Iterator produces a traits.Iterator which walks over the map keys.
//
// The Iterator is frequently used within comprehensions.
func (m *MapValue) Iterator() traits.Iterator {
	keys := make([]ref.Val, len(m.fieldMap))
	i := 0
	for k := range m.fieldMap {
		keys[i] = types.String(k)
		i++
	}
	return &baseMapIterator{
		baseVal: &baseVal{},
		keys:    keys,
	}
}

// Size returns the number of keys in the map.
func (m *MapValue) Size() ref.Val {
	return types.Int(len(m.Fields))
}

// Type returns the CEL ref.Type for the map.
func (m *MapValue) Type() ref.Type {
	return types.MapType
}

// Value returns the Go-native representation of the MapValue.
func (m *MapValue) Value() interface{} {
	return m
}

type baseMapIterator struct {
	*baseVal
	keys []ref.Val
	idx  int
}

// HasNext implements the traits.Iterator interface method.
func (it *baseMapIterator) HasNext() ref.Val {
	if it.idx < len(it.keys) {
		return types.True
	}
	return types.False
}

// Next implements the traits.Iterator interface method.
func (it *baseMapIterator) Next() ref.Val {
	key := it.keys[it.idx]
	it.idx++
	return key
}

// Type implements the CEL ref.Val interface metohd.
func (it *baseMapIterator) Type() ref.Type {
	return types.IteratorType
}
