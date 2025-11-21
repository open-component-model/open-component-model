package jsonschemagen

import (
	"go/ast"
	"strings"
)

func jsonTagName(f *ast.Field, fallback string) string {
	if f.Tag == nil {
		return fallback
	}
	tag := strings.Trim(f.Tag.Value, "`")
	for _, part := range strings.Split(tag, " ") {
		if strings.HasPrefix(part, `json:"`) {
			val := strings.TrimPrefix(part, `json:"`)
			val = strings.TrimSuffix(val, `"`)
			return strings.Split(val, ",")[0]
		}
	}
	return fallback
}

func jsonTagHasOmitEmpty(f *ast.Field) bool {
	if f.Tag == nil {
		return false
	}
	tag := strings.Trim(f.Tag.Value, "`")
	for _, part := range strings.Split(tag, " ") {
		if strings.HasPrefix(part, `json:"`) {
			content := strings.TrimSuffix(strings.TrimPrefix(part, `json:"`), `"`)
			parts := strings.Split(content, ",")
			for _, p := range parts[1:] {
				if p == "omitempty" {
					return true
				}
			}
		}
	}
	return false
}
