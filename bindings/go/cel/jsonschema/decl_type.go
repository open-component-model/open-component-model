package jsonschema

import (
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
)

// DeclType represents the universal type descriptor for JSON Schema types.
type DeclType struct {
	name string
	// Fields contains a map of escaped CEL identifier field names to field declarations.
	Fields      map[string]*DeclField
	KeyType     *DeclType
	ElemType    *DeclType
	TypeParam   bool
	Metadata    map[string]string
	MaxElements uint64
	// MinSerializedSize represents the smallest possible size in bytes that
	// the DeclType could be serialized to in JSON.
	MinSerializedSize uint64

	celType      *cel.Type
	traitMask    int
	defaultValue ref.Val

	*Schema
}

// TypeName returns the fully qualified type name for the DeclType.
func (t *DeclType) TypeName() string {
	return t.name
}

// CelType returns the CEL type of this declaration.
func (t *DeclType) CelType() *cel.Type {
	return t.celType
}

// DefaultValue returns the CEL ref.Val representing the default value for this object type,
// if one exists.
func (t *DeclType) DefaultValue() ref.Val {
	return t.defaultValue
}

// HasTrait implements the CEL ref.Type interface making this type declaration suitable for use
// within the CEL evaluator.
func (t *DeclType) HasTrait(trait int) bool {
	if t.traitMask&trait == trait {
		return true
	}
	if t.defaultValue == nil {
		return false
	}
	_, isDecl := t.defaultValue.Type().(*DeclType)
	if isDecl {
		return false
	}
	return t.defaultValue.Type().HasTrait(trait)
}

// IsMap returns whether the declaration is a 'map' type which defines parameterized key and
// element types, but not fields.
func (t *DeclType) IsMap() bool {
	return t.KeyType != nil && t.ElemType != nil && t.Fields == nil
}
