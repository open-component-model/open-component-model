package decl

import (
	"fmt"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
)

var _ ref.Type = (*Type)(nil)

// Type represents the universal type descriptor for JSON Schema types.
type Type struct {
	name string
	// Fields contains a map of escaped CEL identifier field names to field declarations.
	Fields      map[string]*Field
	KeyType     *Type
	ElemType    *Type
	Metadata    map[string]string
	MaxElements uint64
	// MinSerializedSize represents the smallest possible size in bytes that
	// the DeclType could be serialized to in JSON.
	MinSerializedSize uint64

	celType      *cel.Type
	traitMask    int
	defaultValue ref.Val
}

// TypeName returns the fully qualified type name for the DeclType.
func (t *Type) TypeName() string {
	return t.name
}

// CelType returns the CEL type of this declaration.
func (t *Type) CelType() *cel.Type {
	return t.celType
}

// DefaultValue returns the CEL ref.Val representing the default value for this object type,
// if one exists.
func (t *Type) DefaultValue() ref.Val {
	return t.defaultValue
}

// HasTrait implements the CEL ref.Type interface making this type declaration suitable for use
// within the CEL evaluator.
func (t *Type) HasTrait(trait int) bool {
	if t.traitMask&trait == trait {
		return true
	}
	if t.defaultValue == nil {
		return false
	}
	_, isDecl := t.defaultValue.Type().(*Type)
	if isDecl {
		return false
	}
	return t.defaultValue.Type().HasTrait(trait)
}

// IsMap returns whether the declaration is a 'map' type which defines parameterized key and
// element types, but not fields.
func (t *Type) IsMap() bool {
	return t.KeyType != nil && t.ElemType != nil && t.Fields == nil
}

// IsObject returns whether the declartion is an 'object' type which defined a set of typed fields.
func (t *Type) IsObject() bool {
	return t.KeyType == nil && t.ElemType == nil && t.Fields != nil
}

// IsList returns whether the declaration is a `list` type which defines a parameterized element
// type, but not a parameterized key type or fields.
func (t *Type) IsList() bool {
	return t.KeyType == nil && t.ElemType != nil && t.Fields == nil
}

// MaybeAssignTypeName attempts to set the DeclType name to a fully qualified name, if the type
// is of `object` type.
//
// The DeclType must return true for `IsObject` or this assignment will error.
func (t *Type) MaybeAssignTypeName(name string) *Type {
	if t.IsObject() {
		objUpdated := false
		if t.name != "object" {
			name = t.name
		} else {
			objUpdated = true
		}
		fieldMap := make(map[string]*Field, len(t.Fields))
		for fieldName, field := range t.Fields {
			fieldType := field.Type
			fieldTypeName := fmt.Sprintf("%s.%s", name, fieldName)
			updated := fieldType.MaybeAssignTypeName(fieldTypeName)
			if updated == fieldType {
				fieldMap[fieldName] = field
				continue
			}
			objUpdated = true
			fieldMap[fieldName] = &Field{
				Name:         fieldName,
				Type:         updated,
				Required:     field.Required,
				enumValues:   field.enumValues,
				defaultValue: field.defaultValue,
			}
		}
		if !objUpdated {
			return t
		}
		return &Type{
			name:              name,
			Fields:            fieldMap,
			KeyType:           t.KeyType,
			ElemType:          t.ElemType,
			Metadata:          t.Metadata,
			celType:           cel.ObjectType(name),
			traitMask:         t.traitMask,
			defaultValue:      t.defaultValue,
			MinSerializedSize: t.MinSerializedSize,
		}
	}
	if t.IsMap() {
		elemTypeName := fmt.Sprintf("%s.@elem", name)
		updated := t.ElemType.MaybeAssignTypeName(elemTypeName)
		if updated == t.ElemType {
			return t
		}
		return NewMapType(t.KeyType, updated, t.MaxElements)
	}
	if t.IsList() {
		elemTypeName := fmt.Sprintf("%s.@idx", name)
		updated := t.ElemType.MaybeAssignTypeName(elemTypeName)
		if updated == t.ElemType {
			return t
		}
		return NewListType(updated, t.MaxElements)
	}
	return t
}
