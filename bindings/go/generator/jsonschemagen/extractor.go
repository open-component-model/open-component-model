package jsonschemagen

import (
	"go/ast"
	"strings"
)

func extractStructDoc(ts *ast.TypeSpec, gd *ast.GenDecl) (desc string, deprecated bool) {
	var lines []string

	appendFrom := func(cg *ast.CommentGroup) {
		if cg == nil {
			return
		}
		for _, c := range cg.List {
			line := normalizeCommentLine(c.Text)

			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "+") {
				continue // skip markers
			}
			if isNolintDirective(line) {
				continue
			}

			if strings.HasPrefix(strings.ToLower(line), "deprecated:") ||
				strings.Contains(strings.ToLower(line), "@deprecated") {
				deprecated = true
			}

			lines = append(lines, line)
		}
	}

	appendFrom(ts.Doc)
	if len(lines) == 0 {
		appendFrom(gd.Doc)
	}

	return strings.Join(lines, "\n"), deprecated
}

func extractFieldDoc(f *ast.Field) (desc string, deprecated bool) {
	if f.Doc == nil {
		return "", false
	}

	var out []string

	for _, c := range f.Doc.List {
		line := normalizeCommentLine(c.Text)

		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "+") {
			continue
		}
		if isNolintDirective(line) {
			continue
		}

		if strings.HasPrefix(strings.ToLower(line), "deprecated:") ||
			strings.Contains(strings.ToLower(line), "@deprecated") {
			deprecated = true
		}

		out = append(out, line)
	}

	return strings.Join(out, "\n"), deprecated
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
