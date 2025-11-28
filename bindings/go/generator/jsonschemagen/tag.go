package jsonschemagen

import (
	"go/ast"
	"reflect"
	"strconv"
	"strings"
)

// parseJSONTag returns (name, options).
// For tag `json:"foo,omitempty,string"` → ("foo", ["omitempty","string"])
// For tag `json:"-"` → ("-", nil)
// For missing tag → ("", nil)
func parseJSONTag(f *ast.Field) (string, []string) {
	if f.Tag == nil {
		return "", nil
	}

	// The raw literal includes backticks, e.g. `json:"foo"`
	raw := f.Tag.Value

	// Remove backticks.
	tagRaw, err := strconv.Unquote(raw)
	if err != nil {
		tagRaw = strings.Trim(raw, "`\"")
	}

	j := reflect.StructTag(tagRaw).Get("json")
	if j == "" {
		return "", nil
	}

	parts := strings.Split(j, ",")
	name := parts[0]
	var opts []string
	if len(parts) > 1 {
		opts = parts[1:]
	}
	return name, opts
}

// parseJSONTagWithFieldNameFallback returns (name, options).
// If the JSON tag name is empty, it falls back to the field name.
// For tag `json:"foo,omitempty,string"` → ("foo", ["omitempty","string"])
// For tag `json:"-"` → ("-", nil)
// For missing tag on field Foo → ("Foo", nil)
func parseJSONTagWithFieldNameFallback(f *ast.Field) (string, []string) {
	name, opts := parseJSONTag(f)
	if name == "" {
		return f.Names[0].Name, opts
	}
	return name, opts
}
