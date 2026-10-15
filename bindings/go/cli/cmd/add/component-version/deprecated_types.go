package componentversion

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/jedib0t/go-pretty/v6/text"

	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// typeSchema is the part of a generated JSON schema that carries deprecation:
// the flag on the whole type, and on each accepted "type" value the flag set by
// the enum:deprecated marker plus the hint that says what to write instead,
// which the generator derives from the first enum value unless an
// enum:deprecated:hint marker overrides it.
type typeSchema struct {
	Deprecated bool `json:"deprecated"`
	Properties struct {
		Type struct {
			OneOf []struct {
				Const              string `json:"const"`
				Deprecated         bool   `json:"deprecated"`
				DeprecationMessage string `json:"x-deprecationMessage"`
			} `json:"oneOf"`
		} `json:"type"`
	} `json:"properties"`
}

// isTypeDeprecated reports whether the schema of typ marks that exact name
// deprecated, and the hint it carries, if any. A type the scheme does not know
// or that has no schema is not deprecated: a false warning helps nobody.
func isTypeDeprecated(scheme *runtime.Scheme, typ runtime.Type) (deprecated bool, hint string) {
	obj, err := scheme.NewObject(typ)
	if err != nil {
		return false, ""
	}
	introspectable, ok := obj.(runtime.JSONSchemaIntrospectable)
	if !ok {
		return false, ""
	}
	var schema typeSchema
	if err := json.Unmarshal(introspectable.JSONSchema(), &schema); err != nil {
		return false, ""
	}
	for _, name := range schema.Properties.Type.OneOf {
		if name.Const == typ.String() {
			return name.Deprecated || schema.Deprecated, name.DeprecationMessage
		}
	}
	return schema.Deprecated, ""
}

// deprecatedType is one deprecated access or input type found in a
// constructor, with the hint from its schema, if any. kind is "access" or
// "input" and names the field the type was found in.
type deprecatedType struct {
	kind string
	typ  string
	hint string
}

// findDeprecatedTypes lists the distinct deprecated access and input types in
// spec, in first-use order. Access types are looked up in accessScheme, input
// types in inputScheme. Call it before construction so the result is there
// even when the build fails.
func findDeprecatedTypes(accessScheme, inputScheme *runtime.Scheme, spec *constructorruntime.ComponentConstructor) []deprecatedType {
	var types []deprecatedType
	seen := map[deprecatedType]bool{}
	add := func(kind string, scheme *runtime.Scheme, typed runtime.Typed) {
		if typed == nil {
			return
		}
		deprecated, hint := isTypeDeprecated(scheme, typed.GetType())
		if !deprecated {
			return
		}
		t := deprecatedType{kind: kind, typ: typed.GetType().String(), hint: hint}
		if !seen[t] {
			seen[t] = true
			types = append(types, t)
		}
	}
	for _, component := range spec.Components {
		for _, resource := range component.Resources {
			add("access", accessScheme, resource.Access)
			add("input", inputScheme, resource.Input)
		}
		for _, source := range component.Sources {
			add("access", accessScheme, source.Access)
			add("input", inputScheme, source.Input)
		}
	}
	return types
}

// renderDeprecatedTypeNotice writes one yellow line per deprecated type to w,
// after a blank line, and nothing when types is empty. Meant for stderr so
// stdout stays machine-readable; the color library drops escapes when off.
func renderDeprecatedTypeNotice(w io.Writer, types []deprecatedType) {
	if len(types) == 0 {
		return
	}
	_, _ = fmt.Fprintln(w)
	for _, t := range types {
		msg := "WARNING: " + t.kind + " type " + t.typ + " is deprecated"
		if t.hint != "" {
			msg += ", " + t.hint
		}
		_, _ = fmt.Fprintln(w, text.FgYellow.Sprint(msg))
	}
}
