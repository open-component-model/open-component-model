package universe

import (
	"go/ast"
	"strings"
)

// HasMarker returns true if the struct has the annotation marker
// in either the TypeSpec doc or GenDecl doc.
func HasMarker(ts *ast.TypeSpec, gd *ast.GenDecl, marker string) bool {
	return commentGroupHasMarker(ts.Doc, marker) || commentGroupHasMarker(gd.Doc, marker)
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

// ExtractStructComment extracts the human-readable description from
// either the TypeSpec or GenDecl comment group.
// Removes markers like "+ocm:jsonschema-gen=true" and nolint.
//
// This is equivalent to your previous extractCommentText(), just isolated.
func ExtractStructComment(ts *ast.TypeSpec, gd *ast.GenDecl) string {
	if ts.Doc != nil {
		return extractCommentText(ts.Doc)
	}
	return extractCommentText(gd.Doc)
}

func extractCommentText(cg *ast.CommentGroup) string {
	if cg == nil {
		return ""
	}

	var out []string

	for _, c := range cg.List {
		line := normalizeCommentLine(c.Text)

		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "+") {
			// Skip internal markers like +k8s:deepcopy-gen=true, +ocm:...
			continue
		}
		if isNolintDirective(line) {
			continue
		}

		out = append(out, line)
	}

	return strings.Join(out, "\n")
}

func normalizeCommentLine(text string) string {
	line := strings.TrimSpace(strings.TrimPrefix(text, "//"))
	line = strings.TrimSpace(strings.TrimPrefix(line, "/*"))
	line = strings.TrimSpace(strings.TrimSuffix(line, "*/"))
	return strings.TrimSpace(line)
}

func isNolintDirective(line string) bool {
	l := strings.ToLower(line)
	return strings.HasPrefix(l, "nolint") || strings.HasPrefix(l, "//nolint")
}
