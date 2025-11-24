package jsonschemagen

import (
	"go/ast"
	"reflect"
	"strconv"
	"strings"
)

func jsonTagName(f *ast.Field, fallback string) string {
	if f.Tag == nil {
		return fallback
	}

	// Remove surrounding quotes/backticks from the literal value
	tagRaw, err := strconv.Unquote(f.Tag.Value)
	if err != nil {
		// fallback: trim backticks/quotes manually
		tagRaw = strings.Trim(f.Tag.Value, "`\"")
	}

	j := reflect.StructTag(tagRaw).Get("json")
	if j == "" {
		return fallback
	}

	name := strings.Split(j, ",")[0]
	if name == "" {
		return fallback
	}
	return name
}

func jsonTagHasOmitEmpty(f *ast.Field) bool {
	if f.Tag == nil {
		return false
	}

	// Remove surrounding quotes/backticks from the literal value
	tagRaw, err := strconv.Unquote(f.Tag.Value)
	if err != nil {
		tagRaw = strings.Trim(f.Tag.Value, "`\"")
	}

	j := reflect.StructTag(tagRaw).Get("json")
	if j == "" {
		return false
	}

	parts := strings.Split(j, ",")
	for _, p := range parts[1:] {
		if p == "omitempty" {
			return true
		}
	}
	return false
}
