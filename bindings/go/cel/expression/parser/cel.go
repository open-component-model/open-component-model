package parser

import (
	"errors"
	"strings"
)

const (
	// CEL expressions are enclosed between "${" and "}"
	exprStart = "${"
	exprEnd   = "}"
)

// ErrNestedExpression is returned when an expression contains another
// unescaped expression. Nested expressions are only allowed when the
// inner expression appears inside a string literal, for example:
//
//	${outer("${inner}")}   // allowed
//	${outer(${inner})}     // not allowed
var ErrNestedExpression = errors.New("nested expressions are not allowed unless inside string literals")

// extractExpressions extracts all non-nested CEL expressions from a string.
// It returns an error if it encounters an illegal nested expression.
func extractExpressions(str string) ([]string, error) {
	var expressions []string
	start := 0

	for start < len(str) {
		// Find next "${"
		i := strings.Index(str[start:], exprStart)
		if i == -1 {
			break
		}
		startIdx := start + i

		endIdx := startIdx + len(exprStart)
		braces := 1
		inString := false
		escape := false

		for endIdx < len(str) {
			ch := str[endIdx]

			// Escape handling inside strings
			if escape {
				escape = false
				endIdx++
				continue
			}

			// Detect nested ${ BEFORE handling strings or braces
			if endIdx+1 < len(str) && str[endIdx:endIdx+2] == "${" {
				if inString {
					// Inside string literal → treated as normal text
					endIdx += 2
					continue
				}

				// Outside string literal → forbidden nested expression
				return nil, ErrNestedExpression
			}

			// Inside string literal
			if inString {
				if ch == '\\' {
					escape = true
					endIdx++
					continue
				}
				if ch == '"' {
					inString = false
				}
				endIdx++
				continue
			}

			// Outside string literal

			// Starting a new string literal
			if ch == '"' {
				inString = true
				endIdx++
				continue
			}

			// Normal brace processing
			switch ch {
			case '{':
				braces++
			case '}':
				braces--
				if braces == 0 {
					// Found matching end
					expressions = append(expressions, str[startIdx+len(exprStart):endIdx])
					start = endIdx + 1
					goto nextExpr
				}
			}

			endIdx++
		}

		// If we exit the loop and were still inside a string literal → error
		if inString {
			return nil, ErrNestedExpression
		}

		// Incomplete expression → skip one character after '${'
		start = startIdx + 1

	nextExpr:
	}

	return expressions, nil
}

// isStandaloneExpression checks if input is exactly one full expression.
func isStandaloneExpression(str string) (bool, error) {
	expressions, err := extractExpressions(str)
	if err != nil {
		return false, err
	}
	return len(expressions) == 1 && str == exprStart+expressions[0]+exprEnd, nil
}
