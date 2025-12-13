package jsonschemagen

import (
	"encoding/json"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const BuiltinComment = "this core runtime schema was automatically included by the ocm schema generation tool to allow introspection"

func (g *Generator) builtinRuntimeRaw() *JSONSchemaDraft202012 {
	var raw JSONSchemaDraft202012
	if err := json.Unmarshal(runtime.Raw{}.JSONSchema(), &raw); err != nil {
		panic(err)
	}
	raw.Comment = BuiltinComment
	return &raw
}

func (g *Generator) builtinRuntimeType() *JSONSchemaDraft202012 {
	var raw JSONSchemaDraft202012
	if err := json.Unmarshal(runtime.Type{}.JSONSchema(), &raw); err != nil {
		panic(err)
	}
	raw.Comment = BuiltinComment
	return &raw
}

func (g *Generator) builtinRuntimeTyped() *JSONSchemaDraft202012 {
	return &JSONSchemaDraft202012{
		Schema:      JSONSchemaDraft202012URL,
		Comment:     BuiltinComment,
		Title:       "Typed",
		Description: "Typed is used to hold arbitrary typed objects identified by their Type field",
		Ref:         "#/$defs/ocm.software.open-component-model.bindings.go.runtime.Raw",
		Defs: map[string]*JSONSchemaDraft202012{
			"ocm.software.open-component-model.bindings.go.runtime.Raw": g.builtinRuntimeRaw(),
		},
	}
}
