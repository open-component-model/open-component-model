package jsonschemagen

import (
	"go/ast"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractStructDoc_WithCodeBlock(t *testing.T) {
	ts := &ast.TypeSpec{
		Name: ast.NewIdent("Example"),
		Doc: commentGroup(
			"// +marker",
			"//+marker",
			"/////////////////////////////",
			"//nolint:unused",
			"// nolint:unused",
			"// Primary description",
			"//",
			"// ```",
			"// func demo() {}",
			"// ```",
			"//",
			"// Deprecated: old version",
			"// trailing note",
		),
	}

	gd := &ast.GenDecl{
		Doc: commentGroup("// fallback"),
	}

	desc, deprecated := extractStructDoc(ts, gd)

	require.Equal(t,
		"Primary description\n\n```\nfunc demo() {}\n```\n\nDeprecated: old version\ntrailing note",
		desc,
	)

	require.True(t, deprecated)
}

func TestExtractStructDoc_BlockComments(t *testing.T) {
	ts := &ast.TypeSpec{
		Name: ast.NewIdent("Example"),
		Doc: &ast.CommentGroup{
			List: []*ast.Comment{
				{Text: "/*\nPrimary description\n\nDeprecated: remove soon\ntrailing note\n*/"},
			},
		},
	}

	gd := &ast.GenDecl{
		Doc: commentGroup("// fallback"),
	}

	desc, deprecated := extractStructDoc(ts, gd)

	require.Equal(
		t,
		"Primary description\n\nDeprecated: remove soon\ntrailing note",
		desc,
	)

	require.True(t, deprecated)
}

func TestExtractStructDocFallsBackToGenDecl(t *testing.T) {
	ts := &ast.TypeSpec{ // no doc on type spec
		Name: ast.NewIdent("Example"),
	}

	gd := &ast.GenDecl{
		Doc: commentGroup(
			"// +build ignore",
			"//+build ignore",
			"// nolint",
			"//nolint: test",
			"// From gen decl",
			"/* Additional info @deprecated soon */",
			"// Testing",
			"/*",
			"* some line",
			"*/",
		),
	}

	desc, deprecated := extractStructDoc(ts, gd)

	require.Equal(t, "From gen decl\nAdditional info @deprecated soon\nTesting\nsome line", desc)
	require.True(t, deprecated)
}

func TestExtractFieldDocHandlesDirectivesAndDeprecated(t *testing.T) {
	field := &ast.Field{
		Doc: commentGroup(
			"// +optional",
			"// nolint:tag",
			"// Field comment",
			"//",
			"// Deprecated: not used",
			"// Another line @deprecated soon",
		),
	}

	desc, deprecated := extractFieldDoc(field)

	require.Equal(t, "Field comment\n\nDeprecated: not used\nAnother line @deprecated soon", desc)
	require.True(t, deprecated)
}

func TestExtractFieldDocWhenMissing(t *testing.T) {
	field := &ast.Field{}

	desc, deprecated := extractFieldDoc(field)

	require.Equal(t, "", desc)
	require.False(t, deprecated)
}

func commentGroup(lines ...string) *ast.CommentGroup {
	var comments []*ast.Comment
	for _, line := range lines {
		comments = append(comments, &ast.Comment{Text: line})
	}
	return &ast.CommentGroup{List: comments}
}
