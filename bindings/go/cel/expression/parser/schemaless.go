package parser

import (
	"strings"

	"github.com/google/cel-go/cel"

	"ocm.software/open-component-model/bindings/go/cel/expression/fieldpath"
	"ocm.software/open-component-model/bindings/go/cel/expression/variable"
)

// ParseSchemaless extracts CEL expressions without a schema
func ParseSchemaless(resource map[string]any) ([]variable.FieldDescriptor, error) {
	return parseSchemalessResource(resource, fieldpath.Path{})
}

// parseSchemalessResource is a helper function that recursively
// extracts expressions from a resource. It uses a depth first search to traverse
// the resource and extract expressions from string fields
func parseSchemalessResource(resource any, path fieldpath.Path) ([]variable.FieldDescriptor, error) {
	var expressionsFields []variable.FieldDescriptor
	switch field := resource.(type) {
	case map[string]any:
		for field, value := range field {
			fieldPath := path.AddNamed(field)
			fieldExpressions, err := parseSchemalessResource(value, fieldPath)
			if err != nil {
				return nil, err
			}
			expressionsFields = append(expressionsFields, fieldExpressions...)
		}
	case []any:
		for i, item := range field {
			itemPath := path.AddIndexed(i)
			itemExpressions, err := parseSchemalessResource(item, itemPath)
			if err != nil {
				return nil, err
			}
			expressionsFields = append(expressionsFields, itemExpressions...)
		}
	case string:
		ok, err := IsStandaloneExpression(field)
		if err != nil {
			return nil, err
		}
		if ok {
			expr := strings.TrimPrefix(field, "${")
			expr = strings.TrimSuffix(expr, "}")
			expressionsFields = append(expressionsFields, variable.FieldDescriptor{
				Expressions:          []variable.Expression{{Value: expr}},
				ExpectedType:         cel.DynType, // No schema, so we use dynamic type
				Path:                 path,
				StandaloneExpression: true,
			})
		} else {
			expressions, err := ExtractExpressions(field)
			if err != nil {
				return nil, err
			}
			if len(expressions) > 0 {
				var variableExpressions []variable.Expression
				for _, expression := range expressions {
					// we only extract expressions here, parsing is deferred
					variableExpressions = append(variableExpressions, variable.Expression{Value: expression})
				}
				// String template in schemaless parsing
				// StandaloneExpression=false tells builder to set ExpectedType to cel.StringType
				expressionsFields = append(expressionsFields, variable.FieldDescriptor{
					Expressions:          variableExpressions,
					ExpectedType:         nil, // Builder will set this to cel.StringType
					Path:                 path,
					StandaloneExpression: false, // String template - always string
				})
			}
		}

	default:
		// Ignore other types
	}
	return expressionsFields, nil
}
