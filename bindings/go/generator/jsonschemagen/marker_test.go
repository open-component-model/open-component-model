package jsonschemagen_test

import (
	"go/ast"
	"testing"

	"github.com/stretchr/testify/require"

	. "ocm.software/open-component-model/bindings/go/generator/jsonschemagen"
)

func cg(lines ...string) *ast.CommentGroup {
	list := make([]*ast.Comment, len(lines))
	for i, l := range lines {
		list[i] = &ast.Comment{Text: l}
	}
	return &ast.CommentGroup{List: list}
}

func TestHasMarkerKey(t *testing.T) {
	ts := &ast.TypeSpec{
		Doc: cg("// +ocm:jsonschema-gen=true"),
	}
	gd := &ast.GenDecl{
		Doc: cg("// nothing"),
	}

	require.True(t, HasMarkerKey(ts, gd, BaseMarker))
	require.False(t, HasMarkerKey(
		&ast.TypeSpec{},
		&ast.GenDecl{},
		BaseMarker,
	))
}

func TestExtractMarkers_MultipleFormats(t *testing.T) {
	cg := cg(
		"// +ocm:jsonschema-gen:min=1,max=3",
		"// +ocm:jsonschema-gen:maximum=5",
		"// some other comment",
	)

	m := ExtractMarkers(cg, BaseMarker)

	require.Equal(t, map[string]string{
		"min":     "1",
		"max":     "3",
		"maximum": "5",
	}, m)
}

func TestExtractMarkerMap_MergesTypeAndDecl(t *testing.T) {
	ts := &ast.TypeSpec{
		Doc: cg("// +ocm:jsonschema-gen:min=1"),
	}
	gd := &ast.GenDecl{
		Doc: cg("// +ocm:jsonschema-gen:max=10"),
	}

	m := ExtractMarkerMap(ts, gd, BaseMarker)
	require.Equal(t, map[string]string{
		"min": "1",
		"max": "10",
	}, m)
}

func TestApplyNumericMarkers(t *testing.T) {
	s := &JSONSchemaDraft202012{}
	m := map[string]string{
		"min":              "1",
		"max":              "5",
		"exclusiveMin":     "2",
		"exclusiveMaximum": "10",
		"multipleOf":       "3",
	}

	ApplyNumericMarkers(s, m)

	require.NotNil(t, s.Minimum)
	require.NotNil(t, s.Maximum)
	require.NotNil(t, s.ExclusiveMinimum)
	require.NotNil(t, s.ExclusiveMaximum)

	require.Equal(t, 1.0, *s.Minimum)
	require.Equal(t, 5.0, *s.Maximum)
	require.Equal(t, 2.0, *s.ExclusiveMinimum)
	require.Equal(t, 10.0, *s.ExclusiveMaximum)
}

func TestApplyNumericMarkers_IgnoresInvalid(t *testing.T) {
	s := &JSONSchemaDraft202012{}
	m := map[string]string{
		"min":     "not-a-number",
		"maximum": "100",
	}

	ApplyNumericMarkers(s, m)

	require.Nil(t, s.Minimum)
	require.NotNil(t, s.Maximum)
	require.Equal(t, 100.0, *s.Maximum)
}
