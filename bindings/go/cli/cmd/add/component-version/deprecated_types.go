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
// on each accepted "type" value, the flag set by the enum:deprecated marker and
// the message that says what to write instead, which the generator emits only
// from an explicit enum:deprecated:hint marker.
type typeSchema struct {
	Properties struct {
		Type struct {
			OneOf []typeSpelling `json:"oneOf"`
		} `json:"type"`
	} `json:"properties"`
}

type typeSpelling struct {
	Const              string `json:"const"`
	Deprecated         bool   `json:"deprecated"`
	DeprecationMessage string `json:"x-deprecationMessage"`
}

// hint is the text that says what to write instead of the deprecated spelling
// s: the message from the schema when it has one, otherwise a pointer at the
// first current spelling. A type with no current spelling gets no hint.
func (s typeSpelling) hint(spellings []typeSpelling) string {
	if s.DeprecationMessage != "" {
		return s.DeprecationMessage
	}
	for _, other := range spellings {
		if !other.Deprecated {
			return "use " + other.Const + " instead"
		}
	}
	return ""
}

// isTypeDeprecated reports whether the schema of typ marks that exact spelling
// deprecated, and the hint for it, if any. A type the scheme does not know or
// that has no schema is not deprecated: a false warning helps nobody.
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
	spellings := schema.Properties.Type.OneOf
	for _, s := range spellings {
		if s.Const != typ.String() {
			continue
		}
		if !s.Deprecated {
			return false, ""
		}
		return true, s.hint(spellings)
	}
	return false, ""
}

// findDeprecatedTypes lists one warning per distinct deprecated access or input
// type in spec, in first-use order, such as "access type github is deprecated,
// use Git/v1 instead". Access types are looked up in accessScheme, input types
// in inputScheme. Call it before construction so the result is there even when
// the build fails.
func findDeprecatedTypes(accessScheme, inputScheme *runtime.Scheme, spec *constructorruntime.ComponentConstructor) []string {
	var warnings []string
	seen := map[string]bool{}
	add := func(kind string, scheme *runtime.Scheme, typed runtime.Typed) {
		if typed == nil {
			return
		}
		deprecated, hint := isTypeDeprecated(scheme, typed.GetType())
		if !deprecated {
			return
		}
		warning := kind + " type " + typed.GetType().String() + " is deprecated"
		if hint != "" {
			warning += ", " + hint
		}
		if !seen[warning] {
			seen[warning] = true
			warnings = append(warnings, warning)
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
	return warnings
}

// renderDeprecatedTypeNotice writes one yellow line per warning to w, after a
// blank line, and nothing when warnings is empty. Meant for stderr so stdout
// stays machine-readable; the color library drops escapes when off.
func renderDeprecatedTypeNotice(w io.Writer, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	_, _ = fmt.Fprintln(w)
	for _, warning := range warnings {
		_, _ = fmt.Fprintln(w, text.FgYellow.Sprint("WARNING: "+warning))
	}
}
