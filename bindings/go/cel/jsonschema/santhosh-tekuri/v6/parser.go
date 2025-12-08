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

	"github.com/google/cel-go/cel"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"ocm.software/open-component-model/bindings/go/cel/expression/fieldpath"
	"ocm.software/open-component-model/bindings/go/cel/expression/parser"
	"ocm.software/open-component-model/bindings/go/cel/expression/variable"
)

const (
	schemaTypeAny = "any"
)

func ParseResource(resource map[string]interface{}, schema *jsonschema.Schema) ([]variable.FieldDescriptor, error) {
	// Create DeclType from schema, deriving CEL Type Information.
	declType := NewSchemaDeclType(schema)
	if declType == nil {
		return nil, fmt.Errorf("cannot create type information from schema, unsupported schema structure")
	}
	return ParseResourceFromDeclType(resource, declType)
}

// ParseResourceFromDeclType performs a 3-phase parse:
//  1. Schemaless extraction of all CEL expressions from the resource.
//  2. Schema projection and type annotation of the extracted expressions.
//  3. Validation of the resource against the expression-safe schema.
//
// It returns the list of field descriptors representing the extracted expressions with expected types.
func ParseResourceFromDeclType(resource map[string]interface{}, declType *DeclType) ([]variable.FieldDescriptor, error) {
	// because we modify the scheme of the decl type to be safe for expression-based values,
	// we need to work on a copy of the decl type and its schema.
	mutatedDeclType := *declType
	mutatedScheme := *declType.Schema.Schema
	mutatedDeclType.Schema = &Schema{Schema: &mutatedScheme}
	declType = &mutatedDeclType

	// Phase 1: schemaless extraction
	// for any field with a CEL expression, extract it without schema validation
	schemalessDescs, err := parser.ParseSchemaless(resource)
	if err != nil {
		return nil, err
	}

	if err := validateSchema(declType.Schema.Schema); err != nil {
		return nil, err
	}

	// Phase 2: schema projection + type annotation
	// for every field descriptor we found, derive its expected type from the decl type
	// and make sure that the expression conforms to a validatable scheme that
	// is valid even when the expression is not yet resolved.
	annotated, err := makeSchemaExpressionSafeAndDeriveExpectedTypes(schemalessDescs, declType)
	if err != nil {
		return nil, err
	}

	// Phase 3: validate the resource against the modified schema
	// this ensures that non-CEL values are valid according to the schema.
	if err := declType.Schema.Schema.Validate(resource); err != nil {
		return nil, err
	}

	// Stable sorting based on paths
	slices.SortFunc(annotated, func(a, b variable.FieldDescriptor) int {
		return fieldpath.Compare(a.Path, b.Path)
	})

	return annotated, nil
}

// makeSchemaExpressionSafeAndDeriveExpectedTypes walks the schema for each descriptor and sets ExpectedType.
// It also validates non-CEL values if needed.
// because CEL expressions are always strings, it modifies the schema to expect strings at those paths.
// It returns a new slice of descriptors with the updated ExpectedType. The given declType has its
// schema mutated in place to respect the field descriptors.
func makeSchemaExpressionSafeAndDeriveExpectedTypes(
	descs []variable.FieldDescriptor,
	declType *DeclType,
) ([]variable.FieldDescriptor, error) {
	out := make([]variable.FieldDescriptor, len(descs))
	for i, d := range descs {
		sch, err := lookupSchemaForPath(declType.Schema.Schema, d.Path)
		if err != nil {
			return nil, fmt.Errorf("path %s was not found in the schema: %w", d.Path, err)
		}

		var celType *cel.Type
		if !d.StandaloneExpression {
			// String templates *always* evaluate to strings
			celType = cel.StringType
		} else {
			if declType, err := declType.Resolve(d.Path); err != nil {
				if sch.AdditionalProperties == true {
					// treat unknown fields as dyn
					celType = cel.DynType
				} else {
					// For now, if we are not on a guaranteed path where additionalProperties is allowed,
					// fail out. We can be more lenient as we discover new paths
					return nil, fmt.Errorf("path %s: %w", d.Path, err)
				}
			} else {
				celType = declType.CelType()
			}
		}

		d.ExpectedType = celType
		out[i] = d

		*sch = jsonschema.Schema{
			// Because the expression is always a string
			// we dont expect the actual value but instead
			// the string containing the expression(s).
			Types: TypeForSchema(StringType),
		}
	}
	return out, nil
}

// lookupSchemaForPath returns the schema node that corresponds to a resource path.
func lookupSchemaForPath(schema *jsonschema.Schema, path fieldpath.Path) (*jsonschema.Schema, error) {
	current := schema

	for _, seg := range path {
		// Follow $ref if required
		if current.Ref != nil {
			current = current.Ref
		}

		switch {
		case seg.Index != nil:
			// Array index
			itemSchema, err := getArrayItemSchema(current, path)
			if err != nil {
				return nil, err
			}
			current = itemSchema
		case seg.Name != "":
			// Object property
			next, err := getFieldSchema(current, seg.Name)
			if err != nil {
				return nil, err
			}
			switch t := next.(type) {
			case *jsonschema.Schema:
				current = t
			case bool:
				// AdditionalProperties = true → treat as "any"
				if t {
					current = &jsonschema.Schema{
						Types:                TypeForSchema(schemaTypeAny),
						AdditionalProperties: true,
					}
				} else {
					return nil, fmt.Errorf("field %q not allowed by schema", seg.Name)
				}
			default:
				return nil, fmt.Errorf("invalid schema for field %q", seg.Name)
			}
		}
	}

	// Resolve terminal $ref
	if current != nil && current.Ref != nil {
		return current.Ref, nil
	}
	return current, nil
}

func getArrayItemSchema(schema *jsonschema.Schema, path fieldpath.Path) (*jsonschema.Schema, error) {
	if schema == nil {
		return nil, fmt.Errorf("nil schema for path %s", path)
	}

	// JSON Schema draft uses two fields depending on dialect.
	// Simple case: items: { ... }
	if schema.Items != nil {
		switch items := schema.Items.(type) {
		case *jsonschema.Schema:
			// Single schema for all items
			return items, nil

		case []*jsonschema.Schema:
			// Tuple validation: items=[s1, s2, ...]
			if len(items) == 0 {
				return nil, fmt.Errorf("array schema at %s has empty items list", path)
			}
			// Kubernetes OpenAPI does not use tuple, so we pick items[0]
			return items[0], nil

		default:
			return nil, fmt.Errorf("invalid items type %T at path %s", items, path)
		}
	}

	if schema.Items2020 != nil {
		return schema.Items2020, nil
	}

	return nil, fmt.Errorf("array at %s has no item schema", path)
}

func getFieldSchema(schema *jsonschema.Schema, field string) (any, error) {
	if schema == nil {
		return nil, fmt.Errorf("nil schema when resolving field %q", field)
	}

	// First priority: explicit properties
	if schema.Properties != nil {
		if sub, ok := schema.Properties[field]; ok {
			return sub, nil
		}
	}

	// If AdditionalProperties is present, it defines the schema for unknown fields
	if schema.AdditionalProperties != nil {
		// It can be *jsonschema.Schema or bool
		return schema.AdditionalProperties, nil
	}

	// If AdditionalProperties is absent, default is: additionalProperties = true
	return true, nil
}

func validateSchema(schema *jsonschema.Schema) error {
	if schema == nil {
		return fmt.Errorf("schema is nil")
	}
	hasValidType := schema.Types != nil && len(schema.Types.ToStrings()) > 0
	hasValidOneOf := len(schema.OneOf) != 0
	hasValidAnyOf := len(schema.AnyOf) != 0
	hasValidAdditionalProperties := schema.AdditionalProperties != nil
	isValid := hasValidType || hasValidOneOf || hasValidAnyOf || hasValidAdditionalProperties
	// Ensure the schema has at least one valid construct
	if !isValid {
		return fmt.Errorf("schema has no valid type, OneOf, AnyOf, or AdditionalProperties")
	}
	return nil
}
