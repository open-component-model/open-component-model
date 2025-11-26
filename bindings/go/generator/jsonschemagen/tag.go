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

// jsonTagName returns the json tag's name, including "-".
// If the name is empty (e.g. `json:",omitempty"`), it falls back.
func jsonTagName(f *ast.Field, fallback string) string {
	name, _ := parseJSONTag(f)
	if name == "" {
		return fallback
	}
	return name
}

func jsonTagHasOmitEmpty(f *ast.Field) bool {
	_, opts := parseJSONTag(f)
	if len(opts) == 0 {
		return false
	}

	for _, o := range opts {
		if o == "omitempty" {
			return true
		}
	}
	return false
}
