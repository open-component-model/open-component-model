package jsonschema

import (
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// Provider extends the CEL [types.Provider] interface and provides a JSONSchema-based
// type-system.
type Provider struct {
	registeredTypes map[string]*DeclType

	defaultTypeProvider types.Provider

	recognizeKeywordAsFieldName bool
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
	return []string{}, false
}

func (p *Provider) FindStructFieldType(structType, fieldName string) (*types.FieldType, bool) {
	st, found := p.findDeclType(structType)
	if !found {
		return p.defaultTypeProvider.FindStructFieldType(structType, fieldName)
	}

	f, found := st.Fields[fieldName]
	if p.recognizeKeywordAsFieldName && !found && celReservedSymbols.Has(fieldName) {
		f, found = st.Fields["__"+fieldName+"__"]
	}

	if found {
		ft := f.Type
		expT := ft.CelType()
		return &types.FieldType{
			Type: expT,
		}, true
	}
	// This could be a dynamic map.
	if st.IsMap() {
		et := st.ElemType
		expT := et.CelType()
		return &types.FieldType{
			Type: expT,
		}, true
	}
	return nil, false
}

func (p *Provider) NewValue(structType string, fields map[string]ref.Val) ref.Val {
	//TODO implement me
	panic("implement me")
}

// FindType returns the CPT type description which can be mapped to a CEL type.
func (p *Provider) FindType(typeName string) (*DeclType, bool) {
	if p == nil {
		return nil, false
	}
	return p.findType(typeName)
}

func (p *Provider) findType(typeName string) (*DeclType, bool) {
	declType, found := p.registeredTypes[typeName]
	if found {
		return declType, true
	}
	declType = findScalar(typeName)
	return declType, declType != nil
}

func (p *Provider) findDeclType(typeName string) (*DeclType, bool) {
	declType, found := p.registeredTypes[typeName]
	if found {
		return declType, true
	}
	declType = findScalar(typeName)
	return declType, declType != nil
}
