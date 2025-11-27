package jsonschemagen

const BuiltinComment = "this core runtime schema was automatically included by the ocm schema generation tool to allow introspection"

func (g *Generator) builtinRuntimeRaw() *JSONSchemaDraft202012 {
	return &JSONSchemaDraft202012{
		Schema:      JSONSchemaDraft202012URL,
		Comment:     BuiltinComment,
		ID:          "ocm.software/open-component-model/bindings/go/runtime/schemas/Raw.schema.json",
		Title:       "Raw",
		Description: "Raw is used to hold extensions that dynamically define behavior at runtime",
		Type:        "object",
		Properties: map[string]*JSONSchemaDraft202012{
			"type": {Ref: "#/$defs/" + "ocm.software.open-component-model.bindings.go.runtime.Type"},
		},
		Required:             []string{"type"},
		AdditionalProperties: &SchemaOrBool{Bool: Ptr(true)},
	}
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

func (g *Generator) builtinRuntimeType() *JSONSchemaDraft202012 {
	return &JSONSchemaDraft202012{
		Schema:      JSONSchemaDraft202012URL,
		Comment:     BuiltinComment,
		ID:          "ocm.software/open-component-model/bindings/go/runtime/schemas/Type.schema.json",
		Title:       "Type",
		Description: "Type represents a structured type with an optional version and a name. It is used to identify the type of an object in a versioned API.",
		Type:        "string",
		Pattern:     `^([a-zA-Z0-9][a-zA-Z0-9.]*)(?:/(v[0-9]+(?:alpha[0-9]+|beta[0-9]+)?))?$`,
	}
}
