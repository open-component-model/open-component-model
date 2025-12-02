package provider

import (
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"ocm.software/open-component-model/bindings/go/cel/jsonschema"
	"ocm.software/open-component-model/bindings/go/cel/jsonschema/decl"
)

// New returns a JSON Schema-based type-system which is CEL compatible.
func New(rootTypes ...*decl.Type) *Provider {
	typs := allTypesForDecl(rootTypes...)
	return &Provider{
		registeredTypes:     typs,
		defaultTypeProvider: types.NewEmptyRegistry(),
	}
}

func allTypesForDecl(rootTypes ...*decl.Type) map[string]*decl.Type {
	types := make(map[string]*decl.Type)
	for _, t := range rootTypes {
		if t == nil {
			continue
		}
		collectDeclTypes(t.TypeName(), t, types)
	}
	return types
}

// collectDeclTypes adds the given type and all nested types to types.
func collectDeclTypes(path string, t *decl.Type, types map[string]*decl.Type) {
	if t == nil {
		return
	}

	// Object types
	if t.IsObject() {
		// Prefer the declared type name as the logical path if it's not the generic "object".
		if t.TypeName() != "object" {
			path = t.TypeName()
		}

		// Register the object type under its type name.
		types[t.TypeName()] = t

		// Recurse into fields.
		for name, field := range t.Fields {
			collectDeclTypes(path+"."+name, field.Type, types)
		}
		return
	}

	// Map element types
	if t.IsMap() {
		types[path] = t
		collectDeclTypes(path+".@elem", t.ElemType, types)
		return
	}

	// List element types
	if t.IsList() {
		types[path] = t
		collectDeclTypes(path+".@idx", t.ElemType, types)
		return
	}
}

// Provider extends the CEL [types.Provider] interface and provides a JSONSchema-based
// type-system.
type Provider struct {
	registeredTypes map[string]*decl.Type

	defaultTypeProvider types.Provider

	recognizeKeywordAsFieldName bool

	enums map[string]ref.Val
}

var _ types.Provider = (*Provider)(nil)

func (p *Provider) EnumValue(enumName string) ref.Val {
	return p.defaultTypeProvider.EnumValue(enumName)
}

func (p *Provider) FindIdent(identName string) (ref.Val, bool) {
	return p.defaultTypeProvider.FindIdent(identName)
}

func (p *Provider) SetRecognizeKeywordAsFieldName(recognize bool) {
	p.recognizeKeywordAsFieldName = recognize
}

// FindStructType attempts to resolve the typeName provided from the rule's rule-schema, or if not
// from the embedded ref.TypeProvider.
//
// FindStructType overrides the default type-finding behavior of the embedded TypeProvider.
//
// Note, when the type name is based on the Open API Schema, the name will reflect the object path
// where the type definition appears.
func (p *Provider) FindStructType(structType string) (*types.Type, bool) {
	if p == nil {
		return nil, false
	}
	declType, found := p.findType(structType)
	if found {
		expT := declType.CelType()
		return types.NewTypeTypeWithParam(expT), found
	}
	return p.defaultTypeProvider.FindStructType(structType)
}

func (p *Provider) FindStructFieldNames(structType string) ([]string, bool) {
	st, found := p.findDeclType(structType)
	if !found {
		return p.defaultTypeProvider.FindStructFieldNames(structType)
	}

	// If this is a map type, we do NOT return the element object's fields.
	// Returning any names would imply the map has static field names, which it does not.
	if st.IsMap() {
		// No static field names on map objects.
		return []string{}, true
	}

	names := make([]string, 0, len(st.Fields))
	for name := range st.Fields {
		names = append(names, name)
	}
	return names, true
}

func (p *Provider) FindStructFieldType(structType, fieldName string) (*types.FieldType, bool) {
	st, found := p.findDeclType(structType)
	if !found {
		// fallback to base CEL provider
		return p.defaultTypeProvider.FindStructFieldType(structType, fieldName)
	}

	// --- CASE 1: Non-map object with named fields ---
	if !st.IsMap() {
		// standard lookup: instance.<field>
		if f, ok := st.Fields[fieldName]; ok {
			return &types.FieldType{
				Type: f.Type.CelType(),
			}, true
		}

		// support escaped keyword fields, if enabled
		if p.recognizeKeywordAsFieldName && jsonschema.ReservedSymbols.Has(fieldName) {
			if f, ok := st.Fields["__"+fieldName+"__"]; ok {
				return &types.FieldType{
					Type: f.Type.CelType(),
				}, true
			}
		}

		// unknown field on typed object → error
		return nil, false
	}

	// --- CASE 2: Map type ---
	// Dot-access is allowed and treated like key lookup.
	// instance.foo → element type
	elem := st.ElemType
	return &types.FieldType{
		Type: elem.CelType(),
	}, true
}

func (p *Provider) NewValue(typeName string, fields map[string]ref.Val) ref.Val {
	// TODO: implement for JSON Schema types to enable CEL object instantiation
	return p.defaultTypeProvider.NewValue(typeName, fields)
}

// FindType returns the CPT type description which can be mapped to a CEL type.
func (p *Provider) FindType(typeName string) (*decl.Type, bool) {
	if p == nil {
		return nil, false
	}
	return p.findType(typeName)
}

func (p *Provider) findType(typeName string) (*decl.Type, bool) {
	declType, found := p.registeredTypes[typeName]
	if found {
		return declType, true
	}
	declType = decl.Scalar(typeName)
	return declType, declType != nil
}

func (p *Provider) findDeclType(typeName string) (*decl.Type, bool) {
	declType, found := p.registeredTypes[typeName]
	if found {
		return declType, true
	}
	declType = decl.Scalar(typeName)
	return declType, declType != nil
}
