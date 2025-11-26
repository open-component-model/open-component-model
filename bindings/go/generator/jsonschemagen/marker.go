package jsonschemagen

import (
	"go/ast"
	"strconv"
	"strings"
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

func ExtractMarkerMapFromField(cg *ast.CommentGroup, base string) map[string]string {
	return ExtractMarkers(cg, base)
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
		// normalize comment text
		line := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
		line = strings.TrimSpace(strings.TrimPrefix(line, "/*"))
		line = strings.TrimSuffix(line, "*/")
		line = strings.TrimSpace(line)

		// must contain "+ocm:jsonschema-gen:"
		if !strings.Contains(line, base+":") {
			continue
		}

		// split into fields (spaces may separate multiple)
		fields := strings.Fields(line)
		for _, f := range fields {
			if !strings.HasPrefix(f, base+":") {
				continue
			}

			// remove "+ocm:jsonschema-gen:"
			rest := strings.TrimPrefix(f, base+":")

			// allow: key=value,key2=value2
			entries := strings.Split(rest, ",")
			for _, e := range entries {
				kv := strings.SplitN(strings.TrimSpace(e), "=", 2)
				if len(kv) != 2 {
					continue
				}
				key := strings.TrimSpace(kv[0])
				val := strings.TrimSpace(kv[1])
				if key != "" && val != "" {
					out[key] = val
				}
			}
		}
	}

	return out
}

func ApplyNumericMarkers(s *Schema, markers map[string]string) {
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
