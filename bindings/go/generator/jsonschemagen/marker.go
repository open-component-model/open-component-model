package jsonschemagen

import (
	"encoding/json"
	"go/ast"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"ocm.software/open-component-model/bindings/go/generator/universe"
)

const (
	BaseMarker = "+ocm:jsonschema-gen"
)

// HasMarkerKey reports whether a type is annotated with the given marker.
// It checks both the TypeSpec doc and the GenDecl doc.
func HasMarkerKey(ts *ast.TypeSpec, gd *ast.GenDecl, key string) bool {
	return commentGroupHasMarker(ts.Doc, key) ||
		commentGroupHasMarker(gd.Doc, key)
}

func commentGroupHasMarker(cg *ast.CommentGroup, marker string) bool {
	if cg == nil {
		return false
	}
	for _, c := range cg.List {
		if strings.Contains(c.Text, marker) {
			return true
		}
	}
	return false
}

func ExtractMarkerMapFromField(cg *ast.Field, base string) map[string]string {
	if cg == nil {
		return map[string]string{}
	}
	result := map[string]string{}
	if cg.Comment != nil {
		maps.Copy(result, ExtractMarkers(cg.Comment, base))
	}
	if cg.Doc != nil {
		maps.Copy(result, ExtractMarkers(cg.Doc, base))
	}
	return result
}

func ExtractMarkerMap(ts *ast.TypeSpec, gd *ast.GenDecl, base string) map[string]string {
	out := map[string]string{}
	for k, v := range ExtractMarkers(ts.Doc, base) {
		out[k] = v
	}
	for k, v := range ExtractMarkers(gd.Doc, base) {
		out[k] = v
	}
	return out
}

// ExtractMarkers collects all <key>=<value> pairs for a given base marker.
//
// Examples:
//
//	// +ocm:jsonschema-gen:min=1,max=3
//	// +ocm:jsonschema-gen:maximum=5
//
// Returns: {"min":"1","max":"3","maximum":"5"}.
func ExtractMarkers(cg *ast.CommentGroup, base string) map[string]string {
	out := map[string]string{}
	if cg == nil {
		return out
	}

	for _, c := range cg.List {
		line := extractCommentLine(c.Text)

		idx := strings.Index(line, base+":")
		if idx < 0 {
			continue
		}

		rest := strings.TrimSpace(line[idx+len(base+":"):])
		if rest == "" {
			continue
		}

		segments := strings.Split(rest, ",")

		var lastKey string
		for _, seg := range segments {
			seg = strings.TrimSpace(seg)
			if seg == "" {
				continue
			}

			// key=value?
			if kv := strings.SplitN(seg, "=", 2); len(kv) == 2 {
				key := strings.TrimSpace(kv[0])
				val := strings.TrimSpace(kv[1])
				if key == "" || val == "" {
					continue
				}

				// append if key already exists
				if old, ok := out[key]; ok {
					out[key] = old + "," + val
				} else {
					out[key] = val
				}

				lastKey = key
				continue
			}

			// continuation
			if lastKey != "" {
				out[lastKey] = out[lastKey] + "," + seg
			}
		}
	}

	return out
}

func ApplyNumericMarkers(s *JSONSchemaDraft202012, markers map[string]string) {
	if s == nil || len(markers) == 0 {
		return
	}

	// Aliases → canonical JSON Schema keywords
	normalize := map[string]string{
		"min":              "minimum",
		"minimum":          "minimum",
		"max":              "maximum",
		"maximum":          "maximum",
		"exclusiveMin":     "exclusiveMinimum",
		"exclusiveMinimum": "exclusiveMinimum",
		"exclusiveMax":     "exclusiveMaximum",
		"exclusiveMaximum": "exclusiveMaximum",
		"multipleOf":       "multipleOf",
	}

	// Dispatch table to setter functions
	setters := map[string]func(float64){
		"minimum": func(v float64) {
			s.Minimum = Ptr(v)
		},
		"maximum": func(v float64) {
			s.Maximum = Ptr(v)
		},
		"exclusiveMinimum": func(v float64) {
			s.ExclusiveMinimum = Ptr(v)
		},
		"exclusiveMaximum": func(v float64) {
			s.ExclusiveMaximum = Ptr(v)
		},
	}

	for rawKey, rawVal := range markers {
		key, ok := normalize[rawKey]
		if !ok {
			continue
		}

		f, err := strconv.ParseFloat(rawVal, 64)
		if err != nil {
			continue
		}

		if set, exists := setters[key]; exists {
			set(f)
		}
	}
}

func ApplyEnumMarkers(s *JSONSchemaDraft202012, markers map[string]string) {
	if s == nil || len(markers) == 0 {
		return
	}

	rawMain, hasMain := markers["enum"]
	rawDep, hasDep := markers["enum:deprecated"]

	// nothing to do
	if !hasMain && !hasDep {
		return
	}

	// collect all values in order, with dedup:
	// 1) all from enum
	// 2) then any additional from enum:deprecated
	var order []string
	seen := make(map[string]struct{})

	addVals := func(raw string) {
		if raw == "" {
			return
		}
		for _, e := range strings.Split(raw, ",") {
			v := strings.TrimSpace(e)
			if v == "" {
				continue
			}
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			order = append(order, v)
		}
	}

	if hasMain {
		addVals(rawMain)
	}
	if hasDep {
		addVals(rawDep)
	}

	// build deprecated set from enum:deprecated
	depSet := make(map[string]struct{})
	if hasDep {
		for _, e := range strings.Split(rawDep, ",") {
			v := strings.TrimSpace(e)
			if v == "" {
				continue
			}
			depSet[v] = struct{}{}
		}
	}

	var oneOf []*JSONSchemaDraft202012
	for _, v := range order {
		constVal := inferConstValue(string(s.Type), v)

		_, isDeprecated := depSet[v]
		var deprecatedPtr *bool
		if isDeprecated {
			b := true
			deprecatedPtr = &b // omit field when not deprecated
		}

		oneOf = append(oneOf, &JSONSchemaDraft202012{
			Const:      constVal,
			Deprecated: deprecatedPtr,
		})
	}

	if len(oneOf) > 0 {
		s.OneOf = oneOf
	}
}

func inferConstValue(schemaType, raw string) any {
	switch schemaType {
	case "integer":
		if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return i
		}
	case "number":
		if f, err := strconv.ParseFloat(raw, 64); err == nil {
			return f
		}
	}
	return raw
}

func SchemaFromUniverseType(ti *universe.TypeInfo) (string, bool) {
	markers := ExtractMarkerMap(ti.TypeSpec, ti.GenDecl, BaseMarker)
	if markers == nil {
		return "", false
	}
	schemaFrom, ok := markers["schema-from"]
	return schemaFrom, ok && schemaFrom != ""
}

func ApplyFileMarkers(base *JSONSchemaDraft202012, schema string, path string) {
	basePath := filepath.Dir(path)
	source, err := os.OpenInRoot(basePath, schema)
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = source.Close()
	}()
	if err := json.NewDecoder(source).Decode(base); err != nil {
		panic(err)
	}
}
