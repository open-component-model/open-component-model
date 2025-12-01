package jsonschema

import (
	"fmt"
	"reflect"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

func newStructValue() *structValue {
	return &structValue{
		Fields:   []*Field{},
		fieldMap: map[string]*Field{},
	}
}

type structValue struct {
	Fields   []*Field
	fieldMap map[string]*Field
}

// AddField appends a MapField to the MapValue and indexes the field by name.
func (sv *structValue) AddField(field *Field) {
	sv.Fields = append(sv.Fields, field)
	sv.fieldMap[field.Name] = field
}

// ConvertToNative converts the MapValue type to a native go types.
func (sv *structValue) ConvertToNative(typeDesc reflect.Type) (interface{}, error) {
	if typeDesc.Kind() != reflect.Map &&
		typeDesc.Kind() != reflect.Struct &&
		typeDesc.Kind() != reflect.Pointer &&
		typeDesc.Kind() != reflect.Interface {
		return nil, fmt.Errorf("type conversion error from object to '%v'", typeDesc)
	}

	// Unwrap pointers, but track their use.
	isPtr := false
	if typeDesc.Kind() == reflect.Pointer {
		tk := typeDesc
		typeDesc = typeDesc.Elem()
		if typeDesc.Kind() == reflect.Pointer {
			return nil, fmt.Errorf("unsupported type conversion to '%v'", tk)
		}
		isPtr = true
	}

	if typeDesc.Kind() == reflect.Map {
		keyType := typeDesc.Key()
		if keyType.Kind() != reflect.String && keyType.Kind() != reflect.Interface {
			return nil, fmt.Errorf("object fields cannot be converted to type '%v'", keyType)
		}
		elemType := typeDesc.Elem()
		sz := len(sv.fieldMap)
		ntvMap := reflect.MakeMapWithSize(typeDesc, sz)
		for name, val := range sv.fieldMap {
			refVal, err := val.Ref.ConvertToNative(elemType)
			if err != nil {
				return nil, err
			}
			ntvMap.SetMapIndex(reflect.ValueOf(name), reflect.ValueOf(refVal))
		}
		return ntvMap.Interface(), nil
	}

	if typeDesc.Kind() == reflect.Struct {
		ntvObjPtr := reflect.New(typeDesc)
		ntvObj := ntvObjPtr.Elem()
		for name, val := range sv.fieldMap {
			f := ntvObj.FieldByName(name)
			if !f.IsValid() {
				return nil, fmt.Errorf("type conversion error, no such field %s in type %v",
					name, typeDesc)
			}
			fv, err := val.Ref.ConvertToNative(f.Type())
			if err != nil {
				return nil, err
			}
			f.Set(reflect.ValueOf(fv))
		}
		if isPtr {
			return ntvObjPtr.Interface(), nil
		}
		return ntvObj.Interface(), nil
	}
	return nil, fmt.Errorf("type conversion error from object to '%v'", typeDesc)
}

// GetField returns a MapField by name if one exists.
func (sv *structValue) GetField(name string) (*Field, bool) {
	field, found := sv.fieldMap[name]
	return field, found
}

// IsSet returns whether the given field, which is defined, has also been set.
func (sv *structValue) IsSet(key ref.Val) ref.Val {
	k, ok := key.(types.String)
	if !ok {
		return types.MaybeNoSuchOverloadErr(key)
	}
	name := string(k)
	_, found := sv.fieldMap[name]
	return celBool(found)
}
