package jsonschema

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// TypeDoc represents the documentation extracted from a JSON Schema.
type TypeDoc struct {
	Title       string
	Description string
	Properties  []PropertyDoc
}

// PropertyDoc represents a single field or property within a type, potentially nested.
type PropertyDoc struct {
	Name        string
	Path        string // Dot-separated path (e.g., "metadata.name")
	Type        string
	Description string
	Required    bool
	Default     interface{}
	Enum        []interface{}
	ItemsType   string
}

// Extract transforms raw JSON Schema bytes into a TypeDoc.
func Extract(id string, rawData []byte) (*TypeDoc, error) {
	unmarshaled, err := jsonschema.UnmarshalJSON(bytes.NewReader(rawData))
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON schema: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(id, unmarshaled); err != nil {
		return nil, fmt.Errorf("failed to add resource: %w", err)
	}

	s, err := compiler.Compile(id)
	if err != nil {
		return nil, fmt.Errorf("failed to compile schema: %w", err)
	}

	doc := &TypeDoc{
		Title:       s.Title,
		Description: s.Description,
	}

	e := &extractor{
		visited: make(map[*jsonschema.Schema]bool),
	}
	doc.Properties = e.extract(s, "", true)

	// Sort properties by path for consistency
	sort.Slice(doc.Properties, func(i, j int) bool {
		return doc.Properties[i].Path < doc.Properties[j].Path
	})

	return doc, nil
}

// FromType extracts documentation for a specific type from a runtime scheme.
// It returns nil if the type does not implement JSONSchemaIntrospectable.
func FromType(scheme *runtime.Scheme, typ runtime.Type) (*TypeDoc, error) {
	obj, err := scheme.NewObject(typ)
	if err != nil {
		return nil, fmt.Errorf("failed to create object for type %s: %w", typ, err)
	}

	introspectable, ok := obj.(runtime.JSONSchemaIntrospectable)
	if !ok {
		return nil, nil // Or return a specific error if we expect all types to have schemas
	}

	schema := introspectable.JSONSchema()
	if len(schema) == 0 {
		return nil, nil
	}

	return Extract(typ.String(), schema)
}

type extractor struct {
	visited map[*jsonschema.Schema]bool
}

func (e *extractor) extract(s *jsonschema.Schema, path string, required bool) []PropertyDoc {
	if s == nil || e.visited[s] {
		return nil
	}
	e.visited[s] = true
	defer func() { e.visited[s] = false }()

	var props []PropertyDoc

	// Chasing $ref and allOf is still needed to flatten properties into the current level,
	// but the library handles the actual pointer resolution.
	if s.Ref != nil {
		props = append(props, e.extract(s.Ref, path, required)...)
	}
	for _, sub := range s.AllOf {
		props = append(props, e.extract(sub, path, required)...)
	}
	for _, sub := range s.OneOf {
		props = append(props, e.extract(sub, path, false)...)
	}
	for _, sub := range s.AnyOf {
		props = append(props, e.extract(sub, path, false)...)
	}

	// Properties of the current schema
	reqSet := make(map[string]bool)
	for _, r := range s.Required {
		reqSet[r] = true
	}

	for name, prop := range s.Properties {
		fullPath := name
		if path != "" {
			fullPath = path + "." + name
		}

		pd := PropertyDoc{
			Name:        name,
			Path:        fullPath,
			Description: prop.Description,
			Required:    required && reqSet[name],
		}
		if prop.Default != nil {
			pd.Default = *prop.Default
		}

		// Handle type
		if prop.Types != nil && !prop.Types.IsEmpty() {
			pd.Type = prop.Types.ToStrings()[0]
		} else {
			pd.Type = detectType(prop)
		}

		// Handle enum/const
		if prop.Enum != nil {
			pd.Enum = prop.Enum.Values
		}
		if len(pd.Enum) == 0 {
			if prop.Const != nil {
				pd.Enum = []interface{}{*prop.Const}
			} else {
				// Search oneOf for constants
				for _, sub := range prop.OneOf {
					if sub.Const != nil {
						pd.Enum = append(pd.Enum, *sub.Const)
					}
				}
			}
		}

		// Handle array items
		if pd.Type == "array" {
			items := prop.Items2020
			if items == nil {
				if i, ok := prop.Items.(*jsonschema.Schema); ok {
					items = i
				}
			}
			if items != nil && items.Types != nil && !items.Types.IsEmpty() {
				pd.ItemsType = items.Types.ToStrings()[0]
			}
		}

		props = append(props, pd)

		// Recurse for nested objects
		if pd.Type == "object" && len(prop.Properties) > 0 {
			props = append(props, e.extract(prop, fullPath, pd.Required)...)
		}
	}

	return deduplicateProperties(props)
}

func deduplicateProperties(props []PropertyDoc) []PropertyDoc {
	seen := make(map[string]int)
	var result []PropertyDoc
	for _, p := range props {
		if idx, ok := seen[p.Path]; ok {
			// Merge info if needed, or just keep first
			if result[idx].Description == "" && p.Description != "" {
				result[idx].Description = p.Description
			}
			if p.Required {
				result[idx].Required = true
			}
			continue
		}
		seen[p.Path] = len(result)
		result = append(result, p)
	}
	return result
}

func detectType(s *jsonschema.Schema) string {
	if s.Types != nil && !s.Types.IsEmpty() {
		return s.Types.ToStrings()[0]
	}
	if s.Ref != nil {
		return detectType(s.Ref)
	}
	for _, sub := range s.AllOf {
		if t := detectType(sub); t != "" {
			return t
		}
	}
	for _, sub := range s.OneOf {
		if t := detectType(sub); t != "" {
			return t
		}
	}
	for _, sub := range s.AnyOf {
		if t := detectType(sub); t != "" {
			return t
		}
	}
	return ""
}
