package jsonschema

import (
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/google/cel-go/cel"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"

	"ocm.software/open-component-model/bindings/go/cel/expression/fieldpath"
	"ocm.software/open-component-model/bindings/go/cel/expression/parser"
	"ocm.software/open-component-model/bindings/go/cel/expression/variable"
)

func ParseResource(resource map[string]interface{}, schema *jsonschema.Schema) ([]variable.FieldDescriptor, error) {
	// Create DeclType from schema, deriving CEL type information.
	declType := NewSchemaDeclType(schema)
	if declType == nil {
		return nil, fmt.Errorf("cannot create type information from schema, unsupported schema structure")
	}
	return ParseResourceFromDeclType(resource, declType)
}

// ParseResourceFromDeclType performs a 3-phase parse:
//  1. Schemaless extraction of all CEL expressions from the resource.
//  2. Type annotation of the extracted expressions using the DeclType.
//  3. Validation of the resource against the original schema, with
//     expression-related validation errors filtered out.
//
// It returns the list of field descriptors representing the extracted
// expressions with expected types.
func ParseResourceFromDeclType(resource map[string]interface{}, declType *DeclType) ([]variable.FieldDescriptor, error) {
	// Phase 1: schemaless extraction — for any field with a CEL expression,
	// extract it without schema validation.
	schemalessDescs, err := parser.ParseSchemaless(resource)
	if err != nil {
		return nil, err
	}

	// Phase 2: type annotation — for every field descriptor we found, derive
	// its expected CEL type from the DeclType.
	annotated, err := setExpectedTypes(schemalessDescs, declType)
	if err != nil {
		return nil, err
	}

	// Stable sorting based on paths.
	slices.SortFunc(annotated, func(a, b variable.FieldDescriptor) int {
		return fieldpath.Compare(a.Path, b.Path)
	})

	// Phase 3: validate the resource against the original schema. Any
	// validation errors at expression locations are filtered out.
	if err := declType.Schema.Schema.Validate(resource); err != nil {
		err = filterExpressionErrors(err, annotated)
		return annotated, err
	}

	return annotated, nil
}

// setExpectedTypes sets ExpectedType on each descriptor based on the DeclType:
//   - For string templates (non-standalone), the CEL result type is always string.
//   - For standalone expressions, it resolves the path in the DeclType and
//     uses the corresponding CEL type, or dyn if resolution fails. Note that
//     this can be best effort.
func setExpectedTypes(
	descs []variable.FieldDescriptor,
	declType *DeclType,
) ([]variable.FieldDescriptor, error) {
	out := make([]variable.FieldDescriptor, len(descs))
	for i, d := range descs {
		var celType *cel.Type

		if !d.StandaloneExpression {
			// String templates always evaluate to strings.
			celType = cel.StringType
		} else {
			fieldDecl, err := declType.Resolve(d.Path)
			if err != nil {
				return nil, fmt.Errorf(
					"failed to resolve CEL type: expression %q, path %q, root type %q: %w",
					d.Expressions[0].Value,
					d.Path.String(),
					declType.TypeName(),
					err,
				)
			} else {
				celType = fieldDecl.CelType()
			}
		}

		d.ExpectedType = celType
		out[i] = d
	}
	return out, nil
}

// filterExpressionErrors removes validation errors that point to known
// expression locations from the given error. If all errors are filtered out,
// it returns nil.
func filterExpressionErrors(err error, exprDescs []variable.FieldDescriptor) error {
	var valErr *jsonschema.ValidationError
	if !errors.As(err, &valErr) {
		return err
	}

	exprPaths := make(map[string]struct{}, len(exprDescs))
	for _, d := range exprDescs {
		exprPaths[d.Path.String()] = struct{}{}
	}

	if suppressErrors(valErr, exprPaths) {
		return nil
	}
	return valErr
}

// suppressErrors walks the ValidationError tree and removes causes that
// point to known expression locations. It returns true if the error itself
// should be fully suppressed.
func suppressErrors(err *jsonschema.ValidationError, exprPaths map[string]struct{}) bool {
	if err == nil {
		return false
	}

	instPath := instanceLocationToString(err.InstanceLocation)
	if _, ok := exprPaths[instPath]; ok {
		// Directly at an expression location: suppress.
		return true
	}

	// Check if this error is a container-level type error caused by child expressions
	// e.g., array of CEL expressions fails "got array, want null" type check
	if _, isTypeErr := err.ErrorKind.(*kind.Type); isTypeErr && hasChildExpressions(err.InstanceLocation, exprPaths) {
		// This is a type error at a container whose children are expressions
		// Suppress it as it's caused by CEL expressions
		return true
	}

	// Recurse into causes and keep only those that should not be suppressed.
	cleaned := make([]*jsonschema.ValidationError, 0, len(err.Causes))
	for _, c := range err.Causes {
		if !suppressErrors(c, exprPaths) {
			cleaned = append(cleaned, c)
		}
	}
	err.Causes = cleaned

	if len(err.Causes) == 0 {
		// Leaf errors at expression paths can be suppressed for these kinds.
		// there may be other kinds that could be added here as needed.
		// for now we focus on the most common errors that can have leafs:
		// 1. schema errors, which can encompass N actual errors
		// 2. oneOf errors, which can encompass N errors based on each one of branch
		// 3. reference errors, which can encompass 1 error from the referenced schema
		switch err.ErrorKind.(type) {
		case *kind.Schema, *kind.OneOf, *kind.Reference:
			return true
		}
	}

	return false
}

// hasChildExpressions checks if any expression paths are children of the given location
func hasChildExpressions(parentLoc []string, exprPaths map[string]struct{}) bool {
	parentPath := instanceLocationToString(parentLoc)
	// Handle empty path
	if parentPath == "" {
		return false
	}

	parentFP, err := fieldpath.Parse(parentPath)
	if err != nil {
		// If we can't parse the parent path, assume no children
		return false
	}

	for exprPathStr := range exprPaths {
		if exprPathStr == "" {
			continue
		}
		exprFP, err := fieldpath.Parse(exprPathStr)
		if err != nil {
			continue
		}

		// Check if exprFP is a child of parentFP (Path is a []Segment)
		if len(exprFP) > len(parentFP) {
			// Check if the parent path segments match
			matches := true
			for i := range parentFP {
				if exprFP[i].Name != parentFP[i].Name {
					matches = false
					break
				}
				// Also check index if present
				if parentFP[i].Index != nil && exprFP[i].Index != nil {
					if *parentFP[i].Index != *exprFP[i].Index {
						matches = false
						break
					}
				}
			}
			if matches {
				return true
			}
		}
	}
	return false
}

func instanceLocationToString(loc []string) string {
	fp := fieldpath.New()
	for _, l := range loc {
		// Check if this segment is a numeric array index
		if index, err := strconv.Atoi(l); err == nil {
			fp = fp.AddIndexed(index)
		} else {
			fp = fp.AddNamed(l)
		}
	}
	return fp.String()
}
