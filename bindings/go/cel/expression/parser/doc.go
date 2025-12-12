// Package parser provides helpers to locate and validate CEL expressions
// embedded in strings.
//
// CEL expressions are delimited by the literal sequence `${` and a matching `}`.
// The parser recognizes string literals and escape sequences and enforces the
// rule that nested expressions are not allowed unless they appear inside a
// string literal. For example:
//
//   - `${outer("${inner}")}` is allowed
//   - `${outer(${inner})}` is not allowed and will cause ErrNestedExpression
//
// The primary behaviors implemented in this package:
//
//   - ParseSchemaless: returns all CEL expressions found in a schemaless map[string]any structure
package parser
