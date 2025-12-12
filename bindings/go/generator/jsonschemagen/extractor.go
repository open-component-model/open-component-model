package jsonschemagen

import (
	"go/ast"
	"strings"
)

func extractStructDoc(ts *ast.TypeSpec, gd *ast.GenDecl) (_ string, deprecated bool) {
	lines, deprecated := collectDoc(ts.Doc)
	if len(lines) == 0 {
		lines, deprecated = collectDoc(gd.Doc)
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), deprecated
}

func extractFieldDoc(f *ast.Field) (_ string, deprecated bool) {
	var lines []string
	if f.Doc == nil {
		return "", false
	}
	lines, deprecated = collectDoc(f.Doc)
	return strings.TrimSpace(strings.Join(lines, "\n")), deprecated
}

func collectDoc(cg *ast.CommentGroup) (doc []string, deprecated bool) {
	if cg == nil {
		return nil, false
	}
	var lines []string

	for _, c := range cg.List {
		for _, raw := range splitCommentText(c.Text) {
			line := extractCommentLine(raw)

			if skipLine(line) {
				continue
			}

			l := strings.ToLower(line)
			if strings.HasPrefix(l, "deprecated:") || strings.Contains(l, "@deprecated") {
				deprecated = true
			}

			lines = append(lines, line)
		}
	}

	return lines, deprecated
}

func extractCommentLine(s string) string {
	s = strings.TrimSpace(s)

	s = strings.TrimPrefix(s, "//")
	s = strings.TrimPrefix(s, "/*")
	s = strings.TrimSuffix(s, "*/")

	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "*")

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
		lines := splitPreserve(t)

		// remove empty first/last lines caused by block comment markers
		if len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
			lines = lines[1:]
		}
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}

		return lines

	case strings.HasPrefix(t, "//"):
		// keep raw so extractCommentLine can strip markers
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
