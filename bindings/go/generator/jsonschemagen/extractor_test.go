package jsonschemagen

import (
	"go/ast"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractStructDocPrefersTypeSpecDoc(t *testing.T) {
	ts := &ast.TypeSpec{
		Name: ast.NewIdent("Example"),
		Doc: commentGroup(
			"// +marker",
			"// nolint:revive",
			"// Primary description",
			"//",
			"// Deprecated: use another type",
			"// trailing note",
		),
	}

	gd := &ast.GenDecl{
		Doc: commentGroup("// fallback description"),
	}

	desc, deprecated := extractStructDoc(ts, gd)

	require.Equal(t, "Primary description\nDeprecated: use another type\ntrailing note", desc)
	require.True(t, deprecated)
}

func TestExtractStructDocFallsBackToGenDecl(t *testing.T) {
	ts := &ast.TypeSpec{ // no doc on type spec
		Name: ast.NewIdent("Example"),
	}

	gd := &ast.GenDecl{
		Doc: commentGroup(
			"// +build ignore",
			"// nolint",
			"// From gen decl",
			"/* Additional info @deprecated soon */",
		),
	}

	desc, deprecated := extractStructDoc(ts, gd)

	require.Equal(t, "From gen decl\nAdditional info @deprecated soon", desc)
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

	require.Equal(t, "Field comment\nDeprecated: not used\nAnother line @deprecated soon", desc)
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
