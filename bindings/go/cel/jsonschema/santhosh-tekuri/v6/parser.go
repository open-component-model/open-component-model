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
	// we need to work on a copy of reference graph to avoid mutating the original decl type.
	declType = &DeclType{
		Type:   declType.Type,
		Schema: &Schema{Schema: cloneSchemaGraphReferences(declType.Schema.Schema)},
	}

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
		location, err := lookupSchemaForPath(declType.Schema.Schema, d.Path)
		if err != nil {
			return nil, fmt.Errorf("path %s was not found in the schema: %w", d.Path, err)
		}
		var celType *cel.Type
		if !d.StandaloneExpression {
			// String templates *always* evaluate to strings
			celType = cel.StringType
		} else {
			if declType, err := declType.Resolve(d.Path); err != nil {
				if location.Node.AdditionalProperties == true {
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
	}
	return out, nil
}

type schemaLocation struct {
	Parent *jsonschema.Schema // The schema containing the node
	Node   *jsonschema.Schema // The schema node at the path (may be ref)
	Key    string             // Object property name; "" means array item
}

func replaceSchemaAtLocation(loc *schemaLocation, replacement *jsonschema.Schema) {
	// Root node
	if loc.Parent == nil {
		*loc.Node = *replacement
		return
	}

	// Array item
	if loc.Key == "" {
		cloned := *replacement
		loc.Parent.Items2020 = &cloned
		return
	}

	// Object property
	if loc.Parent.Properties == nil {
		loc.Parent.Properties = map[string]*jsonschema.Schema{}
	}
	cloned := *replacement
	loc.Parent.Properties[loc.Key] = &cloned
}

// lookupSchemaForPath returns the schema node that corresponds to a resource path.
func lookupSchemaForPath(root *jsonschema.Schema, path fieldpath.Path) (*schemaLocation, error) {
	current := root
	parent := (*jsonschema.Schema)(nil)
	key := ""

	for _, seg := range path {
		if current == nil {
			return nil, fmt.Errorf("nil schema while resolving %s", path)
		}

		// Follow $ref
		if current.Ref != nil {
			current = current.Ref
		}

		switch {
		case seg.Index != nil:
			item, err := getArrayItemSchema(current, path)
			if err != nil {
				return nil, err
			}
			parent = current
			key = "" // array item
			current = item
		case seg.Name != "":
			raw, err := getFieldSchema(current, seg.Name)
			if err != nil {
				return nil, err
			}
			switch v := raw.(type) {
			case *jsonschema.Schema:
				parent = current
				key = seg.Name
				current = v

			case bool: // AdditionalProperties = true
				if !v {
					return nil, fmt.Errorf("field %q not allowed", seg.Name)
				}
				parent = current
				key = seg.Name
				current = &jsonschema.Schema{
					Types:                TypeForSchema(schemaTypeAny),
					AdditionalProperties: true,
				}
			default:
				return nil, fmt.Errorf("invalid schema for field %q", seg.Name)
			}
		}
	}

	// Resolve final $ref
	if current.Ref != nil {
		current = current.Ref
	}

	return &schemaLocation{
		Parent: parent,
		Node:   current,
		Key:    key,
	}, nil
}

func getArrayItemSchema(schema *jsonschema.Schema, path fieldpath.Path) (*jsonschema.Schema, error) {
	if schema == nil {
		return nil, fmt.Errorf("nil schema for path %s", path)
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
	hasValidRef := schema.Ref != nil
	isValid := hasValidType || hasValidOneOf || hasValidAnyOf || hasValidAdditionalProperties || hasValidRef
	// Ensure the schema has at least one valid construct
	if !isValid {
		return fmt.Errorf("schema has no valid type, OneOf, AnyOf, or AdditionalProperties or $ref")
	}
	return nil
}

// cloneSchemaGraphReferences is a referencial clone that only clones the structure of the schema
// reference pointers, but not other fields.
// we work on this clone to avoid mutating the original schema relations when replacing
// nodes with expression-safe versions.
// because this is not a full clone, it should not be used for other purposes.
func cloneSchemaGraphReferences(s *jsonschema.Schema) *jsonschema.Schema {
	if s == nil {
		return nil
	}
	out := *s // shallow copy of self

	if s.Properties != nil {
		out.Properties = make(map[string]*jsonschema.Schema, len(s.Properties))
		for k, v := range s.Properties {
			out.Properties[k] = cloneSchemaGraphReferences(v)
		}
	}

	if s.AdditionalProperties != nil {
		switch ap := s.AdditionalProperties.(type) {
		case bool:
			out.AdditionalProperties = ap
		case *jsonschema.Schema:
			out.AdditionalProperties = cloneSchemaGraphReferences(ap)
		}
	}

	out.Items2020 = cloneSchemaGraphReferences(s.Items2020)
	out.OneOf = slices.Clone(s.OneOf)
	out.AnyOf = slices.Clone(s.AnyOf)
	out.AllOf = slices.Clone(s.AllOf)
	out.Ref = cloneSchemaGraphReferences(s.Ref)

	return &out
}
