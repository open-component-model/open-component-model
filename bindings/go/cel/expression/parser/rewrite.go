package parser

import "strings"

// RewriteIdentifier replaces the bare identifier ident with replacement everywhere it
// appears as an identifier token in the CEL source expr, leaving occurrences inside
// string literals untouched. An identifier match requires that the preceding character
// is not part of an identifier or a member-access dot (so `resource` is rewritten but
// `myresource` and `x.resource` are not) and that the following character does not
// continue the identifier.
//
// It operates on the source string rather than a parsed AST on purpose: cel-go's
// parser expands macros (e.g. `.filter(...)`) and its unparser cannot faithfully
// round-trip an AST whose macro operands were mutated, so an AST-based rewrite would
// corrupt any expression that references the identifier inside a macro operand. A
// string-level, string-literal-aware scan avoids that entirely.
func RewriteIdentifier(expr, ident, replacement string) string {
	var b strings.Builder
	b.Grow(len(expr))
	var inString byte // 0 when outside a string literal, else the opening quote
	escaped := false
	for i := 0; i < len(expr); i++ {
		c := expr[i]
		if inString != 0 {
			b.WriteByte(c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == inString:
				inString = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			inString = c
			b.WriteByte(c)
			continue
		}
		if isIdentifierStart(c) && strings.HasPrefix(expr[i:], ident) {
			end := i + len(ident)
			prev := byte(0)
			if i > 0 {
				prev = expr[i-1]
			}
			next := byte(0)
			if end < len(expr) {
				next = expr[end]
			}
			if !isIdentifierPart(prev) && prev != '.' && !isIdentifierPart(next) {
				b.WriteString(replacement)
				i = end - 1
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

func isIdentifierStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentifierPart(c byte) bool {
	return isIdentifierStart(c) || (c >= '0' && c <= '9')
}
