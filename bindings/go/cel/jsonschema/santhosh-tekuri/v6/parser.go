// Copyright 2025 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package jsonschema

import (
	"fmt"
	"slices"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"ocm.software/open-component-model/bindings/go/cel/expression/fieldpath"
	"ocm.software/open-component-model/bindings/go/cel/expression/parser"
	"ocm.software/open-component-model/bindings/go/cel/expression/variable"
)

const (
	schemaTypeAny = "any"
)

// ParseResource extracts CEL expressions from a resource based on
// the schema. The resource is expected to be a map[string]interface{}.
//
// Note that this function will also validate the resource against the schema
// and return an error if the resource does not match the schema. When CEL
// expressions are found, they are extracted and returned with the expected
// type of the field (inferred from the schema).
func ParseResource(resource map[string]interface{}, resourceSchema *jsonschema.Schema) ([]variable.FieldDescriptor, error) {
	return parseResource(resource, resourceSchema, nil)
}

// parseResource is a helper function that recursively extracts CEL expressions
// from a resource. It uses a depth first search to traverse the resource and
// extract expressions from string fields
func parseResource(resource interface{}, schema *jsonschema.Schema, path fieldpath.Path) ([]variable.FieldDescriptor, error) {
	if schema.Ref != nil {
		return parseResource(resource, schema.Ref, path)
	}
	if err := validateSchema(schema, path); err != nil {
		return nil, err
	}

	expectedTypes, err := getExpectedTypes(schema)
	if err != nil {
		return nil, err
	}

	switch field := resource.(type) {
	case map[string]interface{}:
		return parseObject(field, schema, path, expectedTypes)
	case []interface{}:
		return parseArray(field, schema, path, expectedTypes)
	case string:
		return parseString(field, schema, path, expectedTypes)
	case nil:
		return nil, nil
	default:
		return parseScalarTypes(field, schema, path, expectedTypes)
	}
}

// getCelType converts an OpenAPI schema to a CEL type using the Kubernetes OpenAPI library.
// This replaces manual type conversion with the library's schema-to-CEL type conversion.
func getCelType(schema *jsonschema.Schema) *cel.Type {
	if schema == nil {
		return cel.DynType
	}

	// Use the library to convert schema to CEL type
	declType := NewSchemaDeclType(schema)
	if declType == nil {
		return cel.DynType
	}

	return declType.CelType()
}

// getExpectedTypes extracts the expected types from a schema for validation purposes.
// This is used for non-CEL values to ensure proper type validation.
func getExpectedTypes(schema *jsonschema.Schema) ([]string, error) {
	// Handle composite schemas (like OneOf, AnyOf)
	if types, found := handleCompositeSchemas(schema); found {
		return types, nil
	}

	// Handle direct type definitions
	//if schema.ID != "" {
	//	return []string{string(schema.ID)}, nil
	//}
	if !schema.Types.IsEmpty() {
		return schema.Types.ToStrings(), nil
	}

	// Handle additional properties
	if isBooleanSchema(schema.AdditionalProperties, true) {
		// NOTE(a-hilaly): I don't like the type "any", we might want to change this to "object"
		// in the future; just haven't really thought about it yet.
		// Basically "any" means that the field can be of any type.
		return []string{schemaTypeAny}, nil
	}

	return nil, fmt.Errorf("unknown schema type")
}

// handleCompositeSchemas processes OneOf and AnyOf schemas
// and returns collected types if present.
func handleCompositeSchemas(schema *jsonschema.Schema) ([]string, bool) {
	// Handle OneOf schemas
	if len(schema.OneOf) > 0 {
		types := collectTypesFromSubSchemas(schema.OneOf)
		if len(types) > 0 {
			return types, true
		}
	}

	// Handle AnyOf schemas
	if len(schema.AnyOf) > 0 {
		types := collectTypesFromSubSchemas(schema.AnyOf)
		if len(types) > 0 {
			return types, true
		}
	}

	return nil, false
}

// collectTypesFromSubSchemas extracts types from a slice of schemas,
// handling structural constraints like Required and Not.
func collectTypesFromSubSchemas(subSchemas []*jsonschema.Schema) []string {
	var types []string

	for _, subSchema := range subSchemas {
		// If there are structural constraints, inject object type
		if len(subSchema.Required) > 0 || subSchema.Not != nil {
			if !slices.Contains(types, "object") {
				types = append(types, "object")
			}
		}
		// Collect types if present
		if !subSchema.Types.IsEmpty() {
			if !slices.Contains(types, subSchema.Types.ToStrings()[0]) {
				types = append(types, subSchema.Types.ToStrings()[0])
			}
		}
	}

	return types
}

func validateSchema(schema *jsonschema.Schema, path fieldpath.Path) error {
	if schema == nil {
		return fmt.Errorf("schema is nil for path %s", path)
	}

	// Ensure the schema has at least one valid construct
	if len(schema.Types.ToStrings()) == 0 && len(schema.OneOf) == 0 && len(schema.AnyOf) == 0 && schema.AdditionalProperties == nil {
		return fmt.Errorf("schema at path %s has no valid type, OneOf, AnyOf, or AdditionalProperties", path)
	}
	return nil
}

func parseObject(field map[string]interface{}, schema *jsonschema.Schema, path fieldpath.Path, expectedTypes []string) ([]variable.FieldDescriptor, error) {
	if !slices.Contains(expectedTypes, "object") && (isBooleanSchema(schema.AdditionalProperties, false)) {
		return nil, fmt.Errorf("expected %s type for path %s, got object", strings.Join(expectedTypes, " or "), path)
	}

	var expressionsFields []variable.FieldDescriptor
	for fieldName, value := range field {
		fieldSchema, err := getFieldSchema(schema, fieldName)
		if err != nil {
			return nil, fmt.Errorf("error getting field schema for path %s: %v", path.AddNamed(fieldName), err)
		}
		fieldPath := path.AddNamed(fieldName)
		if fieldSchemaAsSchema, ok := fieldSchema.(*jsonschema.Schema); ok {
			fieldExpressions, err := parseResource(value, fieldSchemaAsSchema, fieldPath)
			if err != nil {
				return nil, err
			}
			expressionsFields = append(expressionsFields, fieldExpressions...)
		}
	}

	slices.SortFunc(expressionsFields, func(a, b variable.FieldDescriptor) int {
		return fieldpath.Compare(a.Path, b.Path)
	})
	return expressionsFields, nil
}

func parseArray(field []interface{}, schema *jsonschema.Schema, path fieldpath.Path, expectedTypes []string) ([]variable.FieldDescriptor, error) {
	if !slices.Contains(expectedTypes, "array") {
		return nil, fmt.Errorf("expected %s type for path %s, got array", strings.Join(expectedTypes, " or "), path)
	}

	itemSchema, err := getArrayItemSchema(schema, path)
	if err != nil {
		return nil, err
	}

	var expressionsFields []variable.FieldDescriptor
	for i, item := range field {
		itemPath := path.AddIndexed(i)
		itemExpressions, err := parseResource(item, itemSchema, itemPath)
		if err != nil {
			return nil, err
		}
		expressionsFields = append(expressionsFields, itemExpressions...)
	}
	return expressionsFields, nil
}

func parseString(field string, schema *jsonschema.Schema, path fieldpath.Path, expectedTypes []string) ([]variable.FieldDescriptor, error) {
	ok, err := parser.IsStandaloneExpression(field)
	if err != nil {
		return nil, err
	}

	if ok {
		// For CEL expressions, get the CEL type from the schema
		expectedType := getCelType(schema)
		expr := strings.TrimPrefix(field, "${")
		expr = strings.TrimSuffix(expr, "}")
		return []variable.FieldDescriptor{{
			Expressions:          []string{expr},
			ExpectedType:         expectedType,
			Path:                 path,
			StandaloneExpression: true,
		}}, nil
	}

	if !slices.Contains(expectedTypes, "string") && !slices.Contains(expectedTypes, schemaTypeAny) {
		return nil, fmt.Errorf("expected %s type for path %s, got string", strings.Join(expectedTypes, " or "), path)
	}

	expressions, err := parser.ExtractExpressions(field)
	if err != nil {
		return nil, err
	}
	exprs := make([]string, len(expressions))
	for i, expr := range expressions {
		exprs[i] = expr
	}
	if len(expressions) > 0 {
		// String templates always produce strings
		return []variable.FieldDescriptor{{
			Expressions:  exprs,
			ExpectedType: cel.StringType,
			Path:         path,
		}}, nil
	}
	return nil, nil
}

func parseScalarTypes(field interface{}, _ *jsonschema.Schema, path fieldpath.Path, expectedTypes []string) ([]variable.FieldDescriptor, error) {
	// If "any" type is expected, skip validation
	if slices.Contains(expectedTypes, "any") {
		return nil, nil
	}

	// Check if the value matches any of the expected types
	actualType := getSchemaTypeName(field)
	for _, expected := range expectedTypes {
		switch expected {
		case "number":
			if isNumber(field) {
				return nil, nil
			}
		case "int", "integer":
			if isInteger(field) {
				return nil, nil
			}
		case "boolean", "bool":
			if _, ok := field.(bool); ok {
				return nil, nil
			}
		}
	}

	// No match found - return error with all expected types
	return nil, fmt.Errorf("expected %s type for path %s, got %s", strings.Join(expectedTypes, " or "), path, actualType)
}

// getSchemaTypeName converts a Go type to its OpenAPI schema type name
func getSchemaTypeName(v interface{}) string {
	switch v.(type) {
	case bool:
		return "boolean"
	case int, int8, int16, int32, int64:
		return "integer"
	default:
		// For other types (including float), use the Go type name
		return fmt.Sprintf("%T", v)
	}
}

func getFieldSchema(schema *jsonschema.Schema, field string) (any, error) {
	if schema.Properties != nil {
		if fieldSchema, ok := schema.Properties[field]; ok {
			return fieldSchema, nil
		}
	}

	if isBooleanSchema(schema.AdditionalProperties, true) {
		return schema.AdditionalProperties, nil
	}

	return nil, fmt.Errorf("schema not found for field %s", field)
}

func getArrayItemSchema(schema *jsonschema.Schema, path fieldpath.Path) (*jsonschema.Schema, error) {
	if schema.Items != nil {
		return schema.Items2020, nil
	}
	if schema.Items != nil && len(schema.Items2020.Properties) > 0 {
		return schema.Items2020, nil
	}
	return nil, fmt.Errorf("invalid array schema for path %s: neither Items.Schema nor Properties are defined", path)
}

func isNumber(v interface{}) bool {
	return isInteger(v) || isFloat(v)
}

func isFloat(v interface{}) bool {
	switch v.(type) {
	case float32, float64:
		return true
	default:
		return false
	}
}

func isInteger(v interface{}) bool {
	switch v.(type) {
	case int, int8, int32, int64:
		return true
	default:
		return false
	}
}

//
//// joinPathAndField appends a field name to a path. If the fieldName contains
//// a dot or is empty, the path will be appended using ["fieldName"] instead of
//// .fieldName to avoid ambiguity and simplify parsing back the path.
//func joinPathAndFieldName(path, fieldName string) string {
//	if fieldName == "" || strings.Contains(fieldName, ".") {
//		return fmt.Sprintf("%s[%q]", path, fieldName)
//	}
//	if path == "" {
//		return fieldName
//	}
//	return fmt.Sprintf("%s.%s", path, fieldName)
//}

func isBooleanSchema(sch any, val bool) bool {
	b, ok := sch.(bool)
	return ok && (b == val)
}
