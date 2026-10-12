package componentversion

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/jedib0t/go-pretty/v6/text"

	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Deprecation is read from the JSON schema generated for each type. A
// "Deprecated:" paragraph in a type's doc comment lands in the schema as a
// deprecated flag on the whole type, which "ocm describe types" shows as a
// warning. The +ocm:jsonschema-gen:enum:deprecated marker flags single
// spellings of the type, and the optional +ocm:jsonschema-gen:enum:deprecated:hint
// marker adds the text that tells the user what to write instead. Checking the
// schema keeps a single source of truth and needs no registration step: a
// plugin deprecates a type by changing its doc and markers and regenerating.

// typeSchema is the part of a generated schema that carries deprecation: the
// flag on the whole type and the flag and hint on each accepted spelling of
// "type".
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

// isTypeDeprecated reports whether typ, as spelled, is marked deprecated in the
// JSON schema of the object scheme builds for it, and the hint the schema
// carries for that spelling, if any. A type the scheme does not know, or one
// without an embedded schema, is not deprecated: the CLI cannot know, and a
// false warning would train users to ignore the real ones.
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
	for _, spelling := range schema.Properties.Type.OneOf {
		if spelling.Const == typ.String() {
			return spelling.Deprecated || schema.Deprecated, spelling.DeprecationMessage
		}
	}
	return schema.Deprecated, ""
}

// deprecatedAccessType is one deprecated access type spelling found in a
// constructor, with the hint from its schema when the type has one.
type deprecatedAccessType struct {
	typ  string
	hint string
}

// findDeprecatedAccessTypes lists the distinct access type spellings in spec
// that scheme marks deprecated, in the order the constructor first uses them.
// Call it before construction so the result is available even when the build
// fails later.
// Sources are included because they never reach a digest processor or resource
// repository during construction. Elements fed by an input have no access and
// are skipped.
func findDeprecatedAccessTypes(scheme *runtime.Scheme, spec *constructorruntime.ComponentConstructor) []deprecatedAccessType {
	var types []deprecatedAccessType
	seen := map[string]struct{}{}
	add := func(access runtime.Typed) {
		if access == nil {
			return
		}
		deprecated, hint := isTypeDeprecated(scheme, access.GetType())
		if !deprecated {
			return
		}
		typ := access.GetType().String()
		if _, ok := seen[typ]; ok {
			return
		}
		seen[typ] = struct{}{}
		types = append(types, deprecatedAccessType{typ: typ, hint: hint})
	}
	for i := range spec.Components {
		component := &spec.Components[i]
		for j := range component.Resources {
			add(component.Resources[j].Access)
		}
		for j := range component.Sources {
			add(component.Sources[j].Access)
		}
	}
	return types
}

// renderDeprecatedAccessNotice writes a blank line and then one yellow line per
// deprecated type to w. The line carries the schema's hint when the type has
// one and always ends with a pointer to "ocm describe types". It writes
// nothing when types is empty. Meant for stderr, right below the command's
// result, so the result on stdout stays machine-readable. Like the result
// table it does not care whether w is a terminal; the color library drops the
// escape codes on its own when colors are off.
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
