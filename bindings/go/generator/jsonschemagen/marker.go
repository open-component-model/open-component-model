package jsonschemagen

import (
	"go/ast"
	"strings"
)

// HasMarker reports whether a type is annotated with the given marker.
// It checks both the TypeSpec doc and the GenDecl doc.
func HasMarker(ts *ast.TypeSpec, gd *ast.GenDecl, marker string) bool {
	return commentGroupHasMarker(ts.Doc, marker) ||
		commentGroupHasMarker(gd.Doc, marker)
}

func commentGroupHasMarker(cg *ast.CommentGroup, marker string) bool {
	if cg == nil {
		return false
	}
	for _, c := range cg.List {
		if strings.Contains(c.Text, marker) {
			return true
		}
	}
	return false
}
