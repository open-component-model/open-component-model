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

	for i := 0; i < len(str); {
		j := strings.Index(str[i:], exprStart)
		if j == -1 {
			break
		}
		startIdx := i + j

		expr, next, err := scanExpression(str, startIdx)
		if err != nil {
			return nil, err
		}

		// Incomplete expression: scanExpression returns next == startIdx+1.
		// In that case, skip and keep scanning.
		if next == startIdx+1 {
			i = next
			continue
		}

		// Complete expression (possibly empty, e.g. "${}")
		expressions = append(expressions, expr)
		i = next
	}

	return expressions, nil
}

// scanExpression scans a single expression starting at the given "${" position.
// It returns the expression contents (without "${" and "}") and the index to
// continue scanning from.
func scanExpression(str string, startIdx int) (string, int, error) {
	endIdx := startIdx + len(exprStart)
	braces := 1
	var inString *uint8
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
		if inString == nil && endIdx+1 < len(str) && str[endIdx:endIdx+2] == exprStart {
			return "", 0, ErrNestedExpression
		}

		// Inside string literal
		if inString != nil {
			switch ch {
			case '\\':
				escape = true
			case *inString:
				inString = nil
			}
			endIdx++
			continue
		}

		// Outside string literal
		switch ch {
		case '"', '\'':
			inString = &ch
		case '{':
			braces++
		case '}':
			braces--
			if braces == 0 {
				// Found matching end
				return str[startIdx+len(exprStart) : endIdx], endIdx + 1, nil
			}
		}

		endIdx++
	}

	// If we exit the loop and were still inside a string literal → error
	if inString != nil {
		return "", 0, ErrNestedExpression
	}

	// Incomplete expression → skip one character after '$'
	return "", startIdx + 1, nil
}

// isStandaloneExpression checks if input is exactly one full expression.
func isStandaloneExpression(str string) (bool, error) {
	expressions, err := extractExpressions(str)
	if err != nil {
		return false, err
	}
	return len(expressions) == 1 && str == exprStart+expressions[0]+exprEnd, nil
}
