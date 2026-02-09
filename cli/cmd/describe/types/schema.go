package types

import (
	"fmt"
	"slices"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"ocm.software/open-component-model/bindings/go/cel/expression/fieldpath"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// schemaInfo holds the separated components of schema metadata for flexible rendering.
type schemaInfo struct {
	Breadcrumb  string // e.g., "file > path" (empty if at root type level)
	Title       string // e.g., "Path" or "File" (the schema title or type name)
	Type        string // e.g., "file" (the runtime type string)
	Description string // the schema description
	Deprecated  bool
	FieldCount  int
	Required    int
	Optional    int
}

// buildSchemaInfo extracts schema metadata into separate components.
func buildSchemaInfo(typ runtime.Type, schema *jsonschema.Schema, path fieldpath.Path) schemaInfo {
	info := schemaInfo{
		Type:       typ.String(),
		FieldCount: len(schema.Properties),
		Required:   len(schema.Required),
	}
	info.Optional = info.FieldCount - info.Required

	// Title from schema or fall back to type
	if schema.Title != "" {
		info.Title = schema.Title
	} else {
		info.Title = typ.String()
	}

	// Breadcrumb path
	if len(path) > 0 {
		var breadcrumb strings.Builder
		breadcrumb.WriteString(typ.String())
		for _, seg := range path {
			breadcrumb.WriteString(" > ")
			breadcrumb.WriteString(seg.Name)
		}
		info.Breadcrumb = breadcrumb.String()
	}

	// Deprecation
	info.Deprecated = schema.Deprecated || (schema.Ref != nil && schema.Ref.Deprecated)

	// Description
	if schema.Description != "" {
		info.Description = schema.Description
	} else if schema.Ref != nil && schema.Ref.Description != "" {
		info.Description = schema.Ref.Description
	}

	return info
}

// formatTableTitle formats schema info as a table title (for types with properties).
func (s schemaInfo) formatTableTitle() string {
	var parts []string

	// For table title, show breadcrumb OR title with type, not both redundantly
	if s.Breadcrumb != "" {
		parts = append(parts, s.Breadcrumb)
	}

	// Title with type annotation
	if s.Title != s.Type {
		parts = append(parts, fmt.Sprintf("%s (%s)", s.Title, s.Type))
	} else if s.Breadcrumb == "" {
		// Only show type if no breadcrumb (root level)
		parts = append(parts, s.Type)
	}

	if s.Deprecated {
		parts = append(parts, "WARNING: This type is deprecated")
	}

	if s.Description != "" {
		parts = append(parts, s.Description)
	}

	if s.FieldCount > 0 {
		parts = append(parts, fmt.Sprintf("%d fields (%d required, %d optional)", s.FieldCount, s.Required, s.Optional))
	}

	return strings.Join(parts, "\n")
}

// navigateFieldPath navigates into a schema using a field path.
func navigateFieldPath(schema *jsonschema.Schema, path fieldpath.Path) (*jsonschema.Schema, error) {
	current := schema
	for _, segment := range path {
		if segment.Index != nil {
			return nil, fmt.Errorf("indexing not supported for schema path segments")
		}

		if candidate, ok := current.Properties[segment.Name]; ok {
			current = candidate
			continue
		}

		if current.Ref != nil {
			if candidate, ok := current.Ref.Properties[segment.Name]; ok {
				current = candidate
				continue
			}
		}

		return nil, buildPathNotFoundError(current, segment.Name)
	}
	return current, nil
}

// buildPathNotFoundError creates a helpful error message when a path segment is not found.
func buildPathNotFoundError(schema *jsonschema.Schema, segmentName string) error {
	var availablePaths []string
	for propName := range schema.Properties {
		availablePaths = append(availablePaths, propName)
	}
	if schema.Ref != nil {
		for propName := range schema.Ref.Properties {
			if !slices.Contains(availablePaths, propName) {
				availablePaths = append(availablePaths, propName)
			}
		}
	}
	slices.Sort(availablePaths)

	errMsg := fmt.Sprintf("schema path segment %q not found", segmentName)
	if len(availablePaths) > 0 {
		errMsg += fmt.Sprintf("\n\nAvailable fields at this level:\n  %s", strings.Join(availablePaths, "\n  "))
	}
	return fmt.Errorf("%s", errMsg)
}

// getPropertyTypeString returns a human-readable type string for a schema property.
func getPropertyTypeString(prop *jsonschema.Schema) string {
	ts := ""
	if prop.Types != nil {
		ts = prop.Types.String()
	}
	if ts == "" && prop.Ref != nil && prop.Ref.Types != nil {
		ts = prop.Ref.Types.String()
	}

	if strings.Contains(ts, "object") && (len(prop.Properties) > 0 || (prop.Ref != nil && len(prop.Ref.Properties) > 0)) {
		ts += " →"
	}

	return ts
}

// getPropertyDescription returns the description for a schema property.
func getPropertyDescription(prop *jsonschema.Schema) string {
	desc := prop.Description
	if desc == "" && prop.Ref != nil {
		desc = prop.Ref.Description
	}

	propEnum := prop.Enum
	if propEnum == nil && prop.Ref != nil {
		propEnum = prop.Ref.Enum
	}
	if propEnum != nil {
		desc += "\nPossible values: " + fmt.Sprintf("%v", prop.Enum.Values)
	}

	var oneOfDesc []string
	if prop.OneOf != nil {
		for _, of := range prop.OneOf {
			if of.Const != nil {
				oneOfDesc = append(oneOfDesc, fmt.Sprintf("%v", *of.Const))
			}
		}
	}
	if prop.Ref != nil && prop.Ref.OneOf != nil {
		for _, of := range prop.Ref.OneOf {
			if of.Const != nil {
				oneOfDesc = append(oneOfDesc, fmt.Sprintf("%v", *of.Const))
			}
		}
	}
	if len(oneOfDesc) > 0 {
		desc += "\nPossible values: " + fmt.Sprintf("%v", oneOfDesc)
	}

	return desc
}

// getFieldConstraints returns only the constraint info (enums, oneOf) without the base description.
func getFieldConstraints(schema *jsonschema.Schema) string {
	var parts []string

	propEnum := schema.Enum
	if propEnum == nil && schema.Ref != nil {
		propEnum = schema.Ref.Enum
	}
	if propEnum != nil {
		parts = append(parts, "Possible values: "+fmt.Sprintf("%v", propEnum.Values))
	}

	var oneOfDesc []string
	if schema.OneOf != nil {
		for _, of := range schema.OneOf {
			if of.Const != nil {
				oneOfDesc = append(oneOfDesc, fmt.Sprintf("%v", *of.Const))
			}
		}
	}
	if schema.Ref != nil && schema.Ref.OneOf != nil {
		for _, of := range schema.Ref.OneOf {
			if of.Const != nil {
				oneOfDesc = append(oneOfDesc, fmt.Sprintf("%v", *of.Const))
			}
		}
	}
	if len(oneOfDesc) > 0 {
		parts = append(parts, "Possible values: "+fmt.Sprintf("%v", oneOfDesc))
	}

	return strings.Join(parts, "\n")
}

// schemaHasProperties returns true if the schema has any child properties.
func schemaHasProperties(schema *jsonschema.Schema) bool {
	if len(schema.Properties) > 0 {
		return true
	}
	if schema.Ref != nil && len(schema.Ref.Properties) > 0 {
		return true
	}
	return false
}

// collectFieldPaths recursively collects all field paths from a schema.
func collectFieldPaths(schema *jsonschema.Schema, prefix string, maxDepth int) []string {
	if schema == nil || maxDepth <= 0 {
		return nil
	}

	var paths []string
	properties := schema.Properties
	if len(properties) == 0 && schema.Ref != nil {
		properties = schema.Ref.Properties
	}

	for propName, prop := range properties {
		currentPath := propName
		if prefix != "" {
			currentPath = prefix + "." + propName
		}
		paths = append(paths, currentPath)

		// Recursively collect nested paths for object types (check if has properties)
		if len(prop.Properties) > 0 {
			paths = append(paths, collectFieldPaths(prop, currentPath, maxDepth-1)...)
		} else if prop.Ref != nil && len(prop.Ref.Properties) > 0 {
			paths = append(paths, collectFieldPaths(prop.Ref, currentPath, maxDepth-1)...)
		}
	}

	slices.Sort(paths)
	return paths
}
