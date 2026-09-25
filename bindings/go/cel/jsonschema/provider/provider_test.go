package provider

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/cel/jsonschema/decl"
)

func TestNewFromTypeMap_ResolvesSameTypesAsNew(t *testing.T) {
	r := require.New(t)

	metaType := decl.NewObjectType("example.meta", map[string]*decl.Field{
		"name": decl.NewField("name", decl.StringType, true, nil, nil),
	})
	rootType := decl.NewObjectType("example.root", map[string]*decl.Field{
		"meta":    decl.NewField("meta", metaType, true, nil, nil),
		"version": decl.NewField("version", decl.IntType, false, nil, nil),
	})

	// Build the merged type map the same way env.Builder does for
	// NewFromTypeMap: one FieldTypeMap walk per schema, merged in order.
	merged := map[string]*decl.Type{}
	for _, root := range []*decl.Type{metaType, rootType} {
		for name, typ := range FieldTypeMap(root.TypeName(), root) {
			merged[name] = typ
		}
	}

	fromNew := New(metaType, rootType)
	fromMap := NewFromTypeMap(merged)

	r.ElementsMatch(fromNew.TypeNames(), fromMap.TypeNames())
	for _, name := range fromNew.TypeNames() {
		expected, ok := fromNew.FindDeclType(name)
		r.True(ok)
		actual, ok := fromMap.FindDeclType(name)
		r.True(ok, "type %q missing in map-backed provider", name)
		r.Same(expected, actual, "both constructors must resolve identical type instances for %q", name)
	}

	// Field resolution must behave identically through both providers.
	for _, p := range []*DeclTypeProvider{fromNew, fromMap} {
		fieldType, found := p.FindStructFieldType("example.root", "meta")
		r.True(found)
		r.Equal(metaType.CelType(), fieldType.Type)
	}
}
