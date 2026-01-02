package jsonschemagen_test

import (
	"encoding/json"
	"go/ast"
	"go/types"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
	"ocm.software/open-component-model/bindings/go/generator/jsonschemagen"
	"ocm.software/open-component-model/bindings/go/generator/universe"
)

func TestSchemaOrBoolMarshalJSONWithBoolTrue(t *testing.T) {
	sb := jsonschemagen.SchemaOrBool{Bool: jsonschemagen.Ptr(true)}

	data, err := json.Marshal(sb)

	require.NoError(t, err)
	require.Equal(t, []byte("true"), data)
}

func TestSchemaOrBoolMarshalJSONWithBoolFalse(t *testing.T) {
	sb := jsonschemagen.SchemaOrBool{Bool: jsonschemagen.Ptr(false)}

	data, err := json.Marshal(sb)

	require.NoError(t, err)
	require.Equal(t, []byte("false"), data)
}

func TestSchemaOrBoolMarshalJSONWithSchema(t *testing.T) {
	schema := &jsonschemagen.JSONSchemaDraft202012{Type: "string"}
	sb := jsonschemagen.SchemaOrBool{Schema: schema}

	data, err := json.Marshal(sb)

	require.NoError(t, err)
	require.Equal(t, []byte(`{"type":"string"}`), data)
}

// --- Helpers for building small Universe/TypeInfo instances for generation tests ---
func mkTypeInfo(pkgPath, typeName string, expr ast.Expr, st *ast.StructType) *universe.TypeInfo {
	filePath := "/tmp/" + typeName + ".go"
	return &universe.TypeInfo{
		Key:      universe.TypeKey{PkgPath: pkgPath, TypeName: typeName},
		Expr:     expr,
		Struct:   st,
		FilePath: filePath,
		TypeSpec: &ast.TypeSpec{Type: expr},
		GenDecl:  &ast.GenDecl{},
		Pkg: &packages.Package{
			Types:     types.NewPackage(pkgPath, "pkg"),
			TypesInfo: &types.Info{Uses: make(map[*ast.Ident]types.Object)},
		},
	}
}

func TestGenerate_PrimitiveAlias(t *testing.T) {
	u := universe.New()
	root := mkTypeInfo("example.com/pkg", "MyString", &ast.Ident{Name: "string"}, nil)
	u.Types[root.Key] = root

	g := jsonschemagen.New(u)
	s := g.GenerateJSONSchemaDraft202012(root)

	require.Equal(t, "string", s.Type)
	require.Equal(t, "example.com/pkg/schemas/MyString.schema.json", s.ID)
	require.Empty(t, s.Defs)
}

func TestGenerate_ArrayAliasItems(t *testing.T) {
	u := universe.New()
	root := mkTypeInfo("example.com/pkg", "MyInts", &ast.ArrayType{Elt: &ast.Ident{Name: "int"}}, nil)
	u.Types[root.Key] = root

	g := jsonschemagen.New(u)
	s := g.GenerateJSONSchemaDraft202012(root)

	require.Equal(t, "array", s.Type)
	require.NotNil(t, s.Items)
	require.Equal(t, "integer", s.Items.Type)
}

func TestGenerate_MapAliasAdditionalProperties(t *testing.T) {
	u := universe.New()
	// referenced type in same package
	other := mkTypeInfo("example.com/pkg", "Other", &ast.Ident{Name: "string"}, nil)
	u.Types[other.Key] = other

	// root is a map[string]Other
	rootExpr := &ast.MapType{Key: &ast.Ident{Name: "string"}, Value: &ast.Ident{Name: "Other"}}
	root := mkTypeInfo("example.com/pkg", "MapOfOther", rootExpr, nil)
	u.Types[root.Key] = root

	g := jsonschemagen.New(u)
	s := g.GenerateJSONSchemaDraft202012(root)

	// AdditionalProperties should be a $ref to Other
	require.NotNil(t, s.AdditionalProperties)
	reqSch := s.AdditionalProperties.Schema
	require.NotNil(t, reqSch)

	expRef := "#/$defs/" + universe.Definition(other.Key)
	require.Equal(t, expRef, reqSch.Ref)

	// defs should contain Other (flattened)
	key := universe.Definition(other.Key)
	val, ok := s.Defs[key]
	require.True(t, ok)
	require.Equal(t, "string", val.Type)
}

func TestGenerate_StructPropertiesAndRequired(t *testing.T) {
	u := universe.New()
	// build a struct with two fields: FieldA (no omitempty) and FieldB (omitempty)
	fieldA := &ast.Field{
		Names: []*ast.Ident{{Name: "FieldA"}},
		Type:  &ast.Ident{Name: "string"},
	}
	fieldB := &ast.Field{
		Names: []*ast.Ident{{Name: "FieldB"}},
		Type:  &ast.Ident{Name: "int"},
		Tag:   &ast.BasicLit{Value: "`json:\"fieldB,omitempty\"`"},
	}
	st := &ast.StructType{Fields: &ast.FieldList{List: []*ast.Field{fieldA, fieldB}}}

	root := mkTypeInfo("example.com/pkg", "MyStruct", nil, st)
	u.Types[root.Key] = root

	g := jsonschemagen.New(u)
	s := g.GenerateJSONSchemaDraft202012(root)

	require.Equal(t, "object", s.Type)
	// properties should include FieldA (name fallback) and fieldB (from tag)
	_, okA := s.Properties["FieldA"]
	_, okB := s.Properties["fieldB"]
	require.True(t, okA)
	require.True(t, okB)

	// Required should only include FieldA (since FieldB has omitempty)
	require.Contains(t, s.Required, "FieldA")
	require.NotContains(t, s.Required, "fieldB")
}

func TestGenerate_MixedFieldTypes(t *testing.T) {
	u := universe.New()
	// register a local referenced type
	local := mkTypeInfo("example.com/pkg", "LocalRef", &ast.Ident{Name: "string"}, nil)
	u.Types[local.Key] = local

	// register an external type and set import map for the root file
	external := mkTypeInfo("example.com/otherpkg", "OtherType", &ast.Ident{Name: "string"}, nil)
	u.Types[external.Key] = external

	// Build struct fields:
	// - Name string
	// - PtrRef *LocalRef
	// - SelRef ext.OtherType
	// - Arr []int
	// - M map[string]*LocalRef
	// - Inline struct { X string }
	// - Omit json:"-"
	// - OmOpt json:"opt,omitempty" (should not be required)
	nameField := &ast.Field{Names: []*ast.Ident{{Name: "Name"}}, Type: &ast.Ident{Name: "string"}}
	ptrField := &ast.Field{Names: []*ast.Ident{{Name: "PtrRef"}}, Type: &ast.StarExpr{X: &ast.Ident{Name: "LocalRef"}}}
	selField := &ast.Field{Names: []*ast.Ident{{Name: "SelRef"}}, Type: &ast.SelectorExpr{X: &ast.Ident{Name: "ext"}, Sel: &ast.Ident{Name: "OtherType"}}}
	arrField := &ast.Field{Names: []*ast.Ident{{Name: "Arr"}}, Type: &ast.ArrayType{Elt: &ast.Ident{Name: "int"}}}
	mapField := &ast.Field{Names: []*ast.Ident{{Name: "M"}}, Type: &ast.MapType{Key: &ast.Ident{Name: "string"}, Value: &ast.StarExpr{X: &ast.Ident{Name: "LocalRef"}}}}
	inlineField := &ast.Field{Names: []*ast.Ident{{Name: "Inline"}}, Type: &ast.StructType{Fields: &ast.FieldList{List: []*ast.Field{{Names: []*ast.Ident{{Name: "X"}}, Type: &ast.Ident{Name: "string"}}}}}}
	omitField := &ast.Field{Names: []*ast.Ident{{Name: "Omit"}}, Type: &ast.Ident{Name: "string"}, Tag: &ast.BasicLit{Value: "`json:\"-\"`"}}
	optField := &ast.Field{Names: []*ast.Ident{{Name: "OmOpt"}}, Type: &ast.Ident{Name: "string"}, Tag: &ast.BasicLit{Value: "`json:\"opt,omitempty\"`"}}

	st := &ast.StructType{Fields: &ast.FieldList{List: []*ast.Field{nameField, ptrField, selField, arrField, mapField, inlineField, omitField, optField}}}

	root := mkTypeInfo("example.com/pkg", "Mixed", nil, st)
	// set the import map for the root's pkg path so selector resolution works
	u.Imports[root.Key.PkgPath] = map[string]string{"ext": "example.com/otherpkg"}

	u.Types[root.Key] = root

	g := jsonschemagen.New(u)
	s := g.GenerateJSONSchemaDraft202012(root)

	require.Equal(t, "object", s.Type)

	// properties exist
	props := s.Properties
	require.Contains(t, props, "Name")
	require.Contains(t, props, "PtrRef")
	require.Contains(t, props, "SelRef")
	require.Contains(t, props, "Arr")
	require.Contains(t, props, "M")
	require.Contains(t, props, "Inline")
	require.NotContains(t, props, "Omit")
	require.Contains(t, props, "opt") // tag name for OmOpt

	// Name is string
	require.Equal(t, "string", props["Name"].Type)

	// PtrRef should be a $ref to local
	require.Equal(t, "#/$defs/"+universe.Definition(local.Key), props["PtrRef"].Ref)

	// SelRef should be a $ref to external
	require.Equal(t, "#/$defs/"+universe.Definition(external.Key), props["SelRef"].Ref)

	// Arr items integer
	require.Equal(t, "array", props["Arr"].Type)
	require.Equal(t, "integer", props["Arr"].Items.Type)

	// M additionalProperties is a $ref to local
	require.NotNil(t, props["M"].AdditionalProperties)
	require.Equal(t, "#/$defs/"+universe.Definition(local.Key), props["M"].AdditionalProperties.Schema.Ref)

	// Inline should be object with property X
	require.Equal(t, "object", props["Inline"].Type)
	require.Contains(t, props["Inline"].Properties, "X")

	// Required should include Name, PtrRef, SelRef, Arr, M, Inline but not opt (omitempty) nor Omit
	req := s.Required
	require.Contains(t, req, "Name")
	require.Contains(t, req, "PtrRef")
	require.Contains(t, req, "SelRef")
	require.Contains(t, req, "Arr")
	require.Contains(t, req, "M")
	require.Contains(t, req, "Inline")
	require.NotContains(t, req, "opt")
	require.NotContains(t, req, "Omit")
}

// --- Additional tests: selector fallback, cycles, builtin runtime schemas ---
func TestSelectorResolutionMissingImportFallsBackToAny(t *testing.T) {
	u := universe.New()
	// external type exists but import map for root is missing
	external := mkTypeInfo("example.com/otherpkg", "OtherType", &ast.Ident{Name: "string"}, nil)
	u.Types[external.Key] = external

	// root struct with selector field ext.OtherType (but no import map)
	selField := &ast.Field{Names: []*ast.Ident{{Name: "SelRef"}}, Type: &ast.SelectorExpr{X: &ast.Ident{Name: "ext"}, Sel: &ast.Ident{Name: "OtherType"}}}
	st := &ast.StructType{Fields: &ast.FieldList{List: []*ast.Field{selField}}}
	root := mkTypeInfo("example.com/pkg", "RootMissingImport", nil, st)
	u.Types[root.Key] = root

	g := jsonschemagen.New(u)
	s := g.GenerateJSONSchemaDraft202012(root)

	p, ok := s.Properties["SelRef"]
	require.True(t, ok)
	// should be anyObjectSchema fallback
	require.Equal(t, "object", p.Type)
	require.NotNil(t, p.AdditionalProperties)
	require.NotNil(t, p.AdditionalProperties.Bool)
	require.True(t, *p.AdditionalProperties.Bool)

	// defs should NOT contain the external type
	_, exists := s.Defs[universe.Definition(external.Key)]
	require.False(t, exists)
}

func TestGenerate_CircularReferencesDoesNotLoopAndFlattensDefs(t *testing.T) {
	u := universe.New()
	// A -> *B, B -> *A
	fieldB := &ast.Field{Names: []*ast.Ident{{Name: "B"}}, Type: &ast.StarExpr{X: &ast.Ident{Name: "B"}}}
	stA := &ast.StructType{Fields: &ast.FieldList{List: []*ast.Field{fieldB}}}
	A := mkTypeInfo("example.com/pkg", "A", nil, stA)

	fieldA := &ast.Field{Names: []*ast.Ident{{Name: "A"}}, Type: &ast.StarExpr{X: &ast.Ident{Name: "A"}}}
	stB := &ast.StructType{Fields: &ast.FieldList{List: []*ast.Field{fieldA}}}
	B := mkTypeInfo("example.com/pkg", "B", nil, stB)

	// register both
	u.Types[A.Key] = A
	u.Types[B.Key] = B

	g := jsonschemagen.New(u)
	s := g.GenerateJSONSchemaDraft202012(A)

	// defs should include B but not A
	keyB := universe.Definition(B.Key)
	_, okB := s.Defs[keyB]
	require.True(t, okB)
	keyA := universe.Definition(A.Key)
	_, okA := s.Defs[keyA]
	require.False(t, okA)

	// B's schema (in defs) should reference A via $ref in its property
	bSch := s.Defs[keyB]
	require.NotNil(t, bSch)
	propA, ok := bSch.Properties["A"]
	require.True(t, ok)
	require.Equal(t, "#/$defs/"+universe.Definition(A.Key), propA.Ref)
}

func TestBuiltinRuntimeSchemas(t *testing.T) {
	u := universe.New()
	g := jsonschemagen.New(u)

	// Raw
	rawTI := &universe.TypeInfo{
		Key: universe.TypeKey{PkgPath: universe.RuntimePackage, TypeName: "Raw"},
		Pkg: &packages.Package{
			Types:     types.NewPackage(universe.RuntimePackage, "runtime"),
			TypesInfo: &types.Info{Uses: make(map[*ast.Ident]types.Object)},
		},
	}
	rawSch := g.GenerateJSONSchemaDraft202012(rawTI)
	require.Equal(t, "object", rawSch.Type)
	require.NotNil(t, rawSch.AdditionalProperties)
	require.NotNil(t, rawSch.AdditionalProperties.Bool)
	require.True(t, *rawSch.AdditionalProperties.Bool)
	require.Contains(t, rawSch.Required, "type")
	require.Equal(t, "#/$defs/ocm.software.open-component-model.bindings.go.runtime.Type", rawSch.Properties["type"].Ref)

	// Type
	typTI := &universe.TypeInfo{
		Key: universe.TypeKey{PkgPath: universe.RuntimePackage, TypeName: "Type"},
		Pkg: &packages.Package{
			Types:     types.NewPackage(universe.RuntimePackage, "runtime"),
			TypesInfo: &types.Info{Uses: make(map[*ast.Ident]types.Object)},
		},
	}
	typSch := g.GenerateJSONSchemaDraft202012(typTI)
	require.Equal(t, "string", typSch.Type)
	require.Equal(t, "ocm.software/open-component-model/bindings/go/runtime/schemas/Type.schema.json", typSch.ID)
	require.NotEmpty(t, typSch.Pattern)
}

func TestGenerate_FieldWithJSONDashExcludedFromRequired(t *testing.T) {
	u := universe.New()
	field := &ast.Field{
		Names: []*ast.Ident{{Name: "Name"}},
		Type:  &ast.Ident{Name: "string"},
		Tag:   &ast.BasicLit{Value: "`json:\"-\"`"},
	}

	st := &ast.StructType{
		Fields: &ast.FieldList{
			List: []*ast.Field{field},
		},
	}

	root := mkTypeInfo("example.com/pkg", "TestStruct", nil, st)
	u.Types[root.Key] = root

	g := jsonschemagen.New(u)
	s := g.GenerateJSONSchemaDraft202012(root)

	require.NotContains(t, s.Required, "-")
}

func TestGenerate_StructInlineFlattensPropertiesAndRequired(t *testing.T) {
	u := universe.New()

	// Base has:
	// - A (required)
	// - b (omitempty)
	baseSt := &ast.StructType{Fields: &ast.FieldList{List: []*ast.Field{
		{Names: []*ast.Ident{{Name: "A"}}, Type: &ast.Ident{Name: "string"}},
		{Names: []*ast.Ident{{Name: "B"}}, Type: &ast.Ident{Name: "string"}, Tag: &ast.BasicLit{Value: "`json:\"b,omitempty\"`"}},
	}}}
	base := mkTypeInfo("example.com/pkg", "Base", nil, baseSt)
	u.Types[base.Key] = base

	// Wrapper has:
	// - Base `json:",inline"`
	// - C (required)
	wrapperSt := &ast.StructType{Fields: &ast.FieldList{List: []*ast.Field{
		{Names: []*ast.Ident{{Name: "Base"}}, Type: &ast.Ident{Name: "Base"}, Tag: &ast.BasicLit{Value: "`json:\",inline\"`"}},
		{Names: []*ast.Ident{{Name: "C"}}, Type: &ast.Ident{Name: "int"}},
	}}}
	wrapper := mkTypeInfo("example.com/pkg", "Wrapper", nil, wrapperSt)
	u.Types[wrapper.Key] = wrapper

	g := jsonschemagen.New(u)
	s := g.GenerateJSONSchemaDraft202012(wrapper)

	require.Equal(t, "object", s.Type)

	// Flattened properties from Base should appear at top-level.
	require.Contains(t, s.Properties, "A")
	require.Contains(t, s.Properties, "b")
	require.Contains(t, s.Properties, "C")

	// The inline field itself must not appear as a property.
	require.NotContains(t, s.Properties, "Base")
	require.NotContains(t, s.Required, "Base")

	// Required must reflect flattened required fields + Wrapper required fields.
	require.Contains(t, s.Required, "A")
	require.Contains(t, s.Required, "C")
	require.NotContains(t, s.Required, "b")
}

func TestGenerate_StructInlinePointerTypeFlattens(t *testing.T) {
	u := universe.New()

	baseSt := &ast.StructType{Fields: &ast.FieldList{List: []*ast.Field{
		{Names: []*ast.Ident{{Name: "A"}}, Type: &ast.Ident{Name: "string"}},
	}}}
	base := mkTypeInfo("example.com/pkg", "Base", nil, baseSt)
	u.Types[base.Key] = base

	wrapperSt := &ast.StructType{Fields: &ast.FieldList{List: []*ast.Field{
		{
			Names: []*ast.Ident{{Name: "Base"}},
			Type:  &ast.StarExpr{X: &ast.Ident{Name: "Base"}},
			Tag:   &ast.BasicLit{Value: "`json:\",inline\"`"},
		},
	}}}
	wrapper := mkTypeInfo("example.com/pkg", "WrapperPtrInline", nil, wrapperSt)
	u.Types[wrapper.Key] = wrapper

	g := jsonschemagen.New(u)
	s := g.GenerateJSONSchemaDraft202012(wrapper)

	require.Equal(t, "object", s.Type)
	require.Contains(t, s.Properties, "A")
	require.NotContains(t, s.Properties, "Base")
	require.Contains(t, s.Required, "A")
	require.NotContains(t, s.Required, "Base")
}

func TestGenerate_StructInlineAndExplicitFieldSameName_ExplicitWinsWhenLater(t *testing.T) {
	u := universe.New()

	// Base contributes "A" as string.
	baseSt := &ast.StructType{Fields: &ast.FieldList{List: []*ast.Field{
		{Names: []*ast.Ident{{Name: "A"}}, Type: &ast.Ident{Name: "string"}},
	}}}
	base := mkTypeInfo("example.com/pkg", "Base", nil, baseSt)
	u.Types[base.Key] = base

	// Wrapper has inline first, then explicit A int.
	wrapperSt := &ast.StructType{Fields: &ast.FieldList{List: []*ast.Field{
		{Names: []*ast.Ident{{Name: "Base"}}, Type: &ast.Ident{Name: "Base"}, Tag: &ast.BasicLit{Value: "`json:\",inline\"`"}},
		{Names: []*ast.Ident{{Name: "A"}}, Type: &ast.Ident{Name: "int"}},
	}}}
	wrapper := mkTypeInfo("example.com/pkg", "WrapperOverride", nil, wrapperSt)
	u.Types[wrapper.Key] = wrapper

	g := jsonschemagen.New(u)
	s := g.GenerateJSONSchemaDraft202012(wrapper)

	require.Contains(t, s.Properties, "A")
	// Explicit field should override inline-provided one if it appears later.
	require.Equal(t, "integer", s.Properties["A"].Type)
}
