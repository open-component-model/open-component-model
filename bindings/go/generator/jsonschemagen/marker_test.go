package jsonschemagen_test

import (
	"go/ast"
	"testing"

	. "ocm.software/open-component-model/bindings/go/generator/jsonschemagen"

	"github.com/stretchr/testify/require"
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

	require.InDelta(t, 1.0, *s.Minimum, 1e-9)
	require.InDelta(t, 5.0, *s.Maximum, 1e-9)
	require.InDelta(t, 2.0, *s.ExclusiveMinimum, 1e-9)
	require.InDelta(t, 10.0, *s.ExclusiveMaximum, 1e-9)
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
	require.InDelta(t, 100.0, *s.Maximum, 1e-9)
}

func TestApplyEnumMarkers(t *testing.T) {
	tests := []struct {
		name    string
		markers map[string]string
		want    []*JSONSchemaDraft202012
	}{
		{
			name:    "enum only",
			markers: map[string]string{"enum": "Git/v1,Git"},
			want: []*JSONSchemaDraft202012{
				{Const: "Git/v1"},
				{Const: "Git"},
			},
		},
		{
			name:    "deprecated spellings follow the current ones and point at the first current one",
			markers: map[string]string{"enum": "Git/v1,Git", "enum:deprecated": "git,git/v1alpha1"},
			want: []*JSONSchemaDraft202012{
				{Const: "Git/v1"},
				{Const: "Git"},
				{Const: "git", Deprecated: Ptr(true), DeprecationMessage: "use Git/v1 instead"},
				{Const: "git/v1alpha1", Deprecated: Ptr(true), DeprecationMessage: "use Git/v1 instead"},
			},
		},
		{
			name:    "no current spelling means no message",
			markers: map[string]string{"enum:deprecated": "GitHub/v1,github"},
			want: []*JSONSchemaDraft202012{
				{Const: "GitHub/v1", Deprecated: Ptr(true)},
				{Const: "github", Deprecated: Ptr(true)},
			},
		},
		{
			name: "hint overrides the default message on every deprecated spelling",
			markers: map[string]string{
				"enum":                 "Git/v1",
				"enum:deprecated":      "GitHub/v1,github",
				"enum:deprecated:hint": "use Git/v1 instead",
			},
			want: []*JSONSchemaDraft202012{
				{Const: "Git/v1"},
				{Const: "GitHub/v1", Deprecated: Ptr(true), DeprecationMessage: "use Git/v1 instead"},
				{Const: "github", Deprecated: Ptr(true), DeprecationMessage: "use Git/v1 instead"},
			},
		},
		{
			name:    "hint without deprecated spellings does nothing",
			markers: map[string]string{"enum": "Git/v1", "enum:deprecated:hint": "use Git/v1 instead"},
			want: []*JSONSchemaDraft202012{
				{Const: "Git/v1"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			s := &JSONSchemaDraft202012{Type: "string"}
			ApplyEnumMarkers(s, tc.markers)
			r.Equal(tc.want, s.OneOf)
		})
	}
}
