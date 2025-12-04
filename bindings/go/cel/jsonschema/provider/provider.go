package provider

import (
	"fmt"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"

	"ocm.software/open-component-model/bindings/go/cel/jsonschema"
	"ocm.software/open-component-model/bindings/go/cel/jsonschema/decl"
)

func allTypesForDecl(declTypes []*decl.Type) map[string]*decl.Type {
	if declTypes == nil {
		return nil
	}
	allTypes := map[string]*decl.Type{}
	for _, declType := range declTypes {
		for k, t := range FieldTypeMap(declType.TypeName(), declType) {
			allTypes[k] = t
		}
	}

	return allTypes
}

// New returns a JSON Schema-based type-system which is CEL compatible.
func New(rootTypes ...*decl.Type) *DeclTypeProvider {
	// Note, if the schema indicates that it's actually based on another proto
	// then prefer the proto definition. For expressions in the proto, a new field
	// annotation will be needed to indicate the expected environment and type of
	// the expression.
	allTypes := allTypesForDecl(rootTypes)
	return &DeclTypeProvider{
		registeredTypes: allTypes,
		typeProvider:    types.NewEmptyRegistry(),
	}
}

// DeclTypeProvider extends the CEL ref.TypeProvider interface and provides an Open API Schema-based
// type-system.
type DeclTypeProvider struct {
	registeredTypes             map[string]*decl.Type
	typeProvider                types.Provider
	typeAdapter                 types.Adapter
	recognizeKeywordAsFieldName bool
}

func (rt *DeclTypeProvider) SetRecognizeKeywordAsFieldName(recognize bool) {
	rt.recognizeKeywordAsFieldName = recognize
}

func (rt *DeclTypeProvider) EnumValue(enumName string) ref.Val {
	return rt.typeProvider.EnumValue(enumName)
}

func (rt *DeclTypeProvider) FindIdent(identName string) (ref.Val, bool) {
	return rt.typeProvider.FindIdent(identName)
}

// EnvOptions returns a set of cel.EnvOption values which includes the declaration set
// as well as a custom ref.TypeProvider.
//
// If the DeclTypeProvider value is nil, an empty []cel.EnvOption set is returned.
func (rt *DeclTypeProvider) EnvOptions(tp types.Provider) ([]cel.EnvOption, error) {
	if rt == nil {
		return []cel.EnvOption{}, nil
	}
	rtWithTypes, err := rt.WithTypeProvider(tp)
	if err != nil {
		return nil, err
	}
	return []cel.EnvOption{
		cel.CustomTypeProvider(rtWithTypes),
		cel.CustomTypeAdapter(rtWithTypes),
	}, nil
}

// WithTypeProvider returns a new DeclTypeProvider that sets the given TypeProvider
// If the original DeclTypeProvider is nil, the returned DeclTypeProvider is still nil.
func (rt *DeclTypeProvider) WithTypeProvider(tp types.Provider) (*DeclTypeProvider, error) {
	if rt == nil {
		return nil, nil
	}
	var ta types.Adapter = types.DefaultTypeAdapter
	tpa, ok := tp.(types.Adapter)
	if ok {
		ta = tpa
	}
	rtWithTypes := &DeclTypeProvider{
		typeProvider:                tp,
		typeAdapter:                 ta,
		registeredTypes:             rt.registeredTypes,
		recognizeKeywordAsFieldName: rt.recognizeKeywordAsFieldName,
	}
	for name, declType := range rt.registeredTypes {
		tpType, found := tp.FindStructType(name)
		// cast celType to types.type

		expT := declType.CelType()
		if found && !expT.IsExactType(tpType) {
			return nil, fmt.Errorf(
				"type %s definition differs between CEL environment and type provider", name)
		}
	}
	return rtWithTypes, nil
}

// FindStructType attempts to resolve the typeName provided from the rule's rule-schema, or if not
// from the embedded ref.TypeProvider.
//
// FindStructType overrides the default type-finding behavior of the embedded TypeProvider.
//
// Note, when the type name is based on the Open API Schema, the name will reflect the object path
// where the type definition appears.
func (rt *DeclTypeProvider) FindStructType(typeName string) (*types.Type, bool) {
	if rt == nil {
		return nil, false
	}
	declType, found := rt.findDeclType(typeName)
	if found {
		expT := declType.CelType()
		return types.NewTypeTypeWithParam(expT), found
	}
	if rt.typeProvider == nil {
		return nil, false
	}
	return rt.typeProvider.FindStructType(typeName)
}

// FindDeclType returns the CPT type description which can be mapped to a CEL type.
func (rt *DeclTypeProvider) FindDeclType(typeName string) (*decl.Type, bool) {
	if rt == nil {
		return nil, false
	}
	return rt.findDeclType(typeName)
}

// FindStructFieldNames returns the field names associated with the type, if the type
// is found.
func (rt *DeclTypeProvider) FindStructFieldNames(typeName string) ([]string, bool) {
	return []string{}, false
}

// FindStructFieldType returns a field type given a type name and field name, if found.
//
// Note, the type name for an Open API Schema type is likely to be its qualified object path.
// If, in the future an object instance rather than a type name were provided, the field
// resolution might more accurately reflect the expected type model. However, in this case
// concessions were made to align with the existing CEL interfaces.
func (rt *DeclTypeProvider) FindStructFieldType(typeName, fieldName string) (*types.FieldType, bool) {
	st, found := rt.findDeclType(typeName)
	if !found {
		return rt.typeProvider.FindStructFieldType(typeName, fieldName)
	}

	f, found := st.Fields[fieldName]
	if rt.recognizeKeywordAsFieldName && !found && jsonschema.ReservedSymbols.Has(fieldName) {
		f, found = st.Fields["__"+fieldName+"__"]
	}

	if found {
		ft := f.Type
		expT := ft.CelType()
		return &types.FieldType{
			Type: expT,
		}, true
	}
	// Handle additionalProperties as a dynamic map.
	if st.AllowsAdditionalProperties() {
		return &types.FieldType{
			Type: types.DynType,
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

// NativeToValue is an implementation of the ref.TypeAdapater interface which supports conversion
// of rule values to CEL ref.Val instances.
func (rt *DeclTypeProvider) NativeToValue(val interface{}) ref.Val {
	return rt.typeAdapter.NativeToValue(val)
}

func (rt *DeclTypeProvider) NewValue(typeName string, fields map[string]ref.Val) ref.Val {
	// TODO: implement to enable CEL object instantiation
	return rt.typeProvider.NewValue(typeName, fields)
}

// TypeNames returns the list of type names declared within the DeclTypeProvider object.
func (rt *DeclTypeProvider) TypeNames() []string {
	typeNames := make([]string, len(rt.registeredTypes))
	i := 0
	for name := range rt.registeredTypes {
		typeNames[i] = name
		i++
	}
	return typeNames
}

func (rt *DeclTypeProvider) findDeclType(typeName string) (*decl.Type, bool) {
	declType, found := rt.registeredTypes[typeName]
	if found {
		return declType, true
	}
	declType = decl.Scalar(typeName)
	return declType, declType != nil
}

// FieldTypeMap constructs a map of the field and object types nested within a given type.
func FieldTypeMap(path string, t *decl.Type) map[string]*decl.Type {
	if t.IsObject() && t.TypeName() != "object" {
		path = t.TypeName()
	}
	types := make(map[string]*decl.Type)
	buildDeclTypes(path, t, types)
	return types
}

func buildDeclTypes(path string, t *decl.Type, types map[string]*decl.Type) {
	// Ensure object types are properly named according to where they appear in the schema.
	if t.IsObject() {
		// Hack to ensure that names are uniquely qualified and work well with the type
		// resolution steps which require fully qualified type names for field resolution
		// to function properly.
		types[t.TypeName()] = t
		for name, field := range t.Fields {
			fieldPath := fmt.Sprintf("%s.%s", path, name)
			buildDeclTypes(fieldPath, field.Type, types)
		}
	}
	// Map element properties to type names if needed.
	if t.IsMap() {
		mapElemPath := fmt.Sprintf("%s.@elem", path)
		buildDeclTypes(mapElemPath, t.ElemType, types)
		types[path] = t
	}
	// List element properties.
	if t.IsList() {
		listIdxPath := fmt.Sprintf("%s.@idx", path)
		buildDeclTypes(listIdxPath, t.ElemType, types)
		types[path] = t
	}
}
