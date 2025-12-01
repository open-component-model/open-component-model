package jsonschema

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// NewListValue returns an empty ListValue instance.
func NewListValue() *ListValue {
	return &ListValue{
		Entries: []*DynValue{},
	}
}

// ListValue contains a list of dynamically typed entries.
type ListValue struct {
	Entries      []*DynValue
	initValueSet sync.Once
	valueSet     map[ref.Val]struct{}
}

// Add concatenates two lists together to produce a new CEL list value.
func (lv *ListValue) Add(other ref.Val) ref.Val {
	oArr, isArr := other.(traits.Lister)
	if !isArr {
		return types.MaybeNoSuchOverloadErr(other)
	}
	szRight := len(lv.Entries)
	szLeft := int(oArr.Size().(types.Int))
	sz := szRight + szLeft
	combo := make([]ref.Val, sz)
	for i := 0; i < szRight; i++ {
		combo[i] = lv.Entries[i].ExprValue()
	}
	for i := 0; i < szLeft; i++ {
		combo[i+szRight] = oArr.Get(types.Int(i))
	}
	return types.DefaultTypeAdapter.NativeToValue(combo)
}

// Append adds another entry into the ListValue.
func (lv *ListValue) Append(entry *DynValue) {
	lv.Entries = append(lv.Entries, entry)
	// The append resets all previously built indices.
	lv.initValueSet = sync.Once{}
}

// Contains returns whether the input `val` is equal to an element in the list.
//
// If any pair-wise comparison between the input value and the list element is an error, the
// operation will return an error.
func (lv *ListValue) Contains(val ref.Val) ref.Val {
	if types.IsUnknownOrError(val) {
		return val
	}
	lv.initValueSet.Do(lv.finalizeValueSet)
	if lv.valueSet != nil {
		_, found := lv.valueSet[val]
		if found {
			return types.True
		}
		// Instead of returning false, ensure that CEL's heterogeneous equality constraint
		// is satisfied by allowing pair-wise equality behavior to determine the outcome.
	}
	var err ref.Val
	sz := len(lv.Entries)
	for i := 0; i < sz; i++ {
		elem := lv.Entries[i]
		cmp := elem.Equal(val)
		b, ok := cmp.(types.Bool)
		if !ok && err == nil {
			err = types.MaybeNoSuchOverloadErr(cmp)
		}
		if b == types.True {
			return types.True
		}
	}
	if err != nil {
		return err
	}
	return types.False
}

// ConvertToNative is an implementation of the CEL ref.Val method used to adapt between CEL types
// and Go-native array-like types.
func (lv *ListValue) ConvertToNative(typeDesc reflect.Type) (interface{}, error) {
	// Non-list conversion.
	if typeDesc.Kind() != reflect.Slice &&
		typeDesc.Kind() != reflect.Array &&
		typeDesc.Kind() != reflect.Interface {
		return nil, fmt.Errorf("type conversion error from list to '%v'", typeDesc)
	}

	// If the list is already assignable to the desired type return it.
	if reflect.TypeOf(lv).AssignableTo(typeDesc) {
		return lv, nil
	}

	// List conversion.
	otherElem := typeDesc.Elem()

	// Allow the element ConvertToNative() function to determine whether conversion is possible.
	sz := len(lv.Entries)
	nativeList := reflect.MakeSlice(typeDesc, int(sz), int(sz))
	for i := 0; i < sz; i++ {
		elem := lv.Entries[i]
		nativeElemVal, err := elem.ConvertToNative(otherElem)
		if err != nil {
			return nil, err
		}
		nativeList.Index(int(i)).Set(reflect.ValueOf(nativeElemVal))
	}
	return nativeList.Interface(), nil
}

// ConvertToType converts the ListValue to another CEL type.
func (lv *ListValue) ConvertToType(t ref.Type) ref.Val {
	switch t {
	case types.ListType:
		return lv
	case types.TypeType:
		return types.ListType
	}
	return types.NewErr("type conversion error from '%s' to '%s'", ListType, t)
}

// Equal returns true if two lists are of the same size, and the values at each index are also
// equal.
func (lv *ListValue) Equal(other ref.Val) ref.Val {
	oArr, isArr := other.(traits.Lister)
	if !isArr {
		return types.MaybeNoSuchOverloadErr(other)
	}
	sz := types.Int(len(lv.Entries))
	if sz != oArr.Size() {
		return types.False
	}
	for i := types.Int(0); i < sz; i++ {
		cmp := lv.Get(i).Equal(oArr.Get(i))
		if cmp != types.True {
			return cmp
		}
	}
	return types.True
}

// Get returns the value at the given index.
//
// If the index is negative or greater than the size of the list, an error is returned.
func (lv *ListValue) Get(idx ref.Val) ref.Val {
	iv, isInt := idx.(types.Int)
	if !isInt {
		return types.ValOrErr(idx, "unsupported index: %v", idx)
	}
	i := int(iv)
	if i < 0 || i >= len(lv.Entries) {
		return types.NewErr("index out of bounds: %v", idx)
	}
	return lv.Entries[i].ExprValue()
}

// Iterator produces a traits.Iterator suitable for use in CEL comprehension macros.
func (lv *ListValue) Iterator() traits.Iterator {
	return &baseListIterator{
		getter: lv.Get,
		sz:     len(lv.Entries),
	}
}

// Size returns the number of elements in the list.
func (lv *ListValue) Size() ref.Val {
	return types.Int(len(lv.Entries))
}

// DeclType returns the CEL ref.DeclType for the list.
func (lv *ListValue) Type() ref.Type {
	return types.ListType
}

// Value returns the Go-native value.
func (lv *ListValue) Value() interface{} {
	return lv
}

// finalizeValueSet inspects the ListValue entries in order to make internal optimizations once all list
// entries are known.
func (lv *ListValue) finalizeValueSet() {
	valueSet := make(map[ref.Val]struct{})
	for _, e := range lv.Entries {
		switch e.value.(type) {
		case bool, float64, int64, string, uint64, types.Null, PlainTextValue:
			valueSet[e.ExprValue()] = struct{}{}
		default:
			lv.valueSet = nil
			return
		}
	}
	lv.valueSet = valueSet
}

type baseListIterator struct {
	*baseVal
	getter func(idx ref.Val) ref.Val
	sz     int
	idx    int
}

func (it *baseListIterator) HasNext() ref.Val {
	if it.idx < it.sz {
		return types.True
	}
	return types.False
}

func (it *baseListIterator) Next() ref.Val {
	v := it.getter(types.Int(it.idx))
	it.idx++
	return v
}

func (it *baseListIterator) Type() ref.Type {
	return types.IteratorType
}
