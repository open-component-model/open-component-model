package componentversion

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/jedib0t/go-pretty/v6/text"

	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Deprecation is read from each type's generated JSON schema: the
// +ocm:jsonschema-gen:enum:deprecated marker flags a type name, and the schema
// carries the text that says what to write instead, which the generator derives
// from the first enum value unless an enum:deprecated:hint marker overrides it.

// typeSchema is the part of a generated schema that carries deprecation: the
// flag on the whole type and the flag and hint on each accepted "type" value.
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

// deprecatedAccessType is one deprecated access type found in a constructor,
// with the hint from its schema, if any.
type deprecatedAccessType struct {
	typ  string
	hint string
}

// findDeprecatedAccessTypes lists the distinct deprecated access types in spec,
// in first-use order. Call it before construction so the result is there even
// when the build fails. Elements fed by an input have no access.
func findDeprecatedAccessTypes(scheme *runtime.Scheme, spec *constructorruntime.ComponentConstructor) []deprecatedAccessType {
	var types []deprecatedAccessType
	seen := map[string]bool{}
	add := func(access runtime.Typed) {
		if access == nil || seen[access.GetType().String()] {
			return
		}
		seen[access.GetType().String()] = true
		if deprecated, hint := isTypeDeprecated(scheme, access.GetType()); deprecated {
			types = append(types, deprecatedAccessType{typ: access.GetType().String(), hint: hint})
		}
	}
	for _, component := range spec.Components {
		for _, resource := range component.Resources {
			add(resource.Access)
		}
		for _, source := range component.Sources {
			add(source.Access)
		}
	}
	return types
}

// renderDeprecatedAccessNotice writes one yellow line per deprecated type to w,
// after a blank line, and nothing when types is empty. Meant for stderr so
// stdout stays machine-readable; the color library drops escapes when off.
func renderDeprecatedAccessNotice(w io.Writer, types []deprecatedAccessType) {
	if len(types) == 0 {
		return
	}
	_, _ = fmt.Fprintln(w)
	for _, t := range types {
		msg := "WARNING: access type " + t.typ + " is deprecated"
		if t.hint != "" {
			msg += ", " + t.hint
		}
		msg += ", run 'ocm describe types access " + t.typ + "' to learn more"
		_, _ = fmt.Fprintln(w, text.FgYellow.Sprint(msg))
	}
}
