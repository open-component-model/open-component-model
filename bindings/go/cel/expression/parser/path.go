package parser

import (
	"fmt"
	"strings"
)

// joinPathAndField appends a field name to a path. If the fieldName contains
// a dot or is empty, the path will be appended using ["fieldName"] instead of
// .fieldName to avoid ambiguity and simplify parsing back the path.
func joinPathAndFieldName(path, fieldName string) string {
	if fieldName == "" || strings.Contains(fieldName, ".") {
		return fmt.Sprintf("%s[%q]", path, fieldName)
	}
	if path == "" {
		return fieldName
	}
	return fmt.Sprintf("%s.%s", path, fieldName)
}
