package jsonschemagen

import (
	"go/ast"
	"strings"
)

func extractStructDoc(ts *ast.TypeSpec, gd *ast.GenDecl) (string, bool) {
	var deprecated bool
	lines := collectDoc(ts.Doc, &deprecated)
	if len(lines) == 0 {
		lines = collectDoc(gd.Doc, &deprecated)
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), deprecated
}

func extractFieldDoc(f *ast.Field) (string, bool) {
	var deprecated bool
	if f.Doc == nil {
		return "", false
	}
	lines := collectDoc(f.Doc, &deprecated)
	return strings.TrimSpace(strings.Join(lines, "\n")), deprecated
}

func collectDoc(cg *ast.CommentGroup, deprecated *bool) []string {
	if cg == nil {
		return nil
	}
	var lines []string

	for _, c := range cg.List {
		for _, raw := range splitCommentText(c.Text) {
			line := extractContent(raw)

			if skipLine(line) {
				continue
			}

			l := strings.ToLower(line)
			if strings.HasPrefix(l, "deprecated:") || strings.Contains(l, "@deprecated") {
				*deprecated = true
			}

			lines = append(lines, line)
		}
	}

	return lines
}

func extractContent(s string) string {
	s = strings.TrimSpace(s)

	// Remove comment markers
	s = strings.TrimPrefix(s, "//")
	s = strings.TrimPrefix(s, "/*")
	s = strings.TrimSuffix(s, "*/")

	return strings.TrimSpace(s)
}

func skipLine(line string) bool {
	if line == "" {
		return false // preserve empty lines
	}

	// Skip marker-like comment lines
	if isNoise(line) {
		return true
	}

	if strings.HasPrefix(line, "+") {
		return true
	}

	if isNolintDirective(line) {
		return true
	}

	return false
}

// Treat punctuation-only comment lines as noise
func isNoise(line string) bool {
	return strings.Trim(line, `/\*-_= `) == ""
}

func splitCommentText(text string) []string {
	t := strings.TrimSpace(text)

	switch {
	case strings.HasPrefix(t, "/*"):
		t = strings.TrimPrefix(t, "/*")
		t = strings.TrimSuffix(t, "*/")
		return splitPreserve(t)

	case strings.HasPrefix(t, "//"):
		// keep raw so extractContent can strip markers
		return []string{t}

	default:
		return splitPreserve(t)
	}
}

func splitPreserve(s string) []string {
	parts := strings.Split(s, "\n")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func isNolintDirective(line string) bool {
	l := strings.ToLower(line)
	return strings.HasPrefix(l, "nolint") || strings.HasPrefix(l, "//nolint")
}
