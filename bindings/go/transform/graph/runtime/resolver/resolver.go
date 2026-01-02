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

package resolver

import (
	"fmt"
	"strings"

	"ocm.software/open-component-model/bindings/go/cel/expression/fieldpath"
	"ocm.software/open-component-model/bindings/go/cel/expression/variable"
)

// ResolutionResult represents the result of resolving a single expression.
type ResolutionResult struct {
	Path     fieldpath.Path
	Resolved bool
	Original string
	Replaced interface{}
	Error    error
}

// ResolutionSummary provides a summary of the resolution process.
type ResolutionSummary struct {
	TotalExpressions    int
	ResolvedExpressions int
	Results             []ResolutionResult
	Errors              []error
}

// Resolver handles the resolution of CEL expressions in Kubernetes resources.
type Resolver struct {
	// The original resource to be resolved. In kro, this will typically
	// be a Kubernetes resource with some fields containing CEL expressions.
	resource map[string]interface{}
	// The data to be used for resolving the expressions. Other systems are
	// responsible for providing this only with available data aka CEL Expressions
	// we've been able to resolve.
	data map[string]interface{}
}

// NewResolver creates a new Resolver instance.
func NewResolver(resource map[string]interface{}, data map[string]interface{}) *Resolver {
	return &Resolver{
		resource: resource,
		data:     data,
	}
}

// Resolve processes all the given ExpressionFields and resolves their CEL expressions.
// It returns a ResolutionSummary containing information about the resolution process.
func (r *Resolver) Resolve(expressions []variable.FieldDescriptor) ResolutionSummary {
	summary := ResolutionSummary{
		TotalExpressions: len(expressions),
		Results:          make([]ResolutionResult, 0, len(expressions)),
	}

	for _, field := range expressions {
		result := r.resolveField(field)
		summary.Results = append(summary.Results, result)
		if result.Resolved {
			summary.ResolvedExpressions++
		}
		if result.Error != nil {
			summary.Errors = append(summary.Errors, result.Error)
		}
	}

	return summary
}

// UpsertValueAtPath sets a value in the resource using the fieldpath parser.
func (r *Resolver) UpsertValueAtPath(path fieldpath.Path, value interface{}) error {
	return r.setValueAtPath(path, value)
}

// resolveField handles the resolution of a single ExpressionField (one field) in
// the resource. It returns a ResolutionResult containing information about the
// resolution process
func (r *Resolver) resolveField(field variable.FieldDescriptor) ResolutionResult {
	result := ResolutionResult{
		Path:     field.Path,
		Original: fmt.Sprintf("%v", field.Expressions),
	}

	value, err := r.getValueFromPath(field.Path)
	if err != nil {
		// Not sure if these kinds of errors should be fatal, these paths are produced
		// by the parser, so they should be valid.
		// Maybe we should log them instead…
		result.Error = fmt.Errorf("error getting value: %w", err)
		return result
	}

	if field.StandaloneExpression {
		resolvedValue, ok := r.data[field.Expressions[0].String()]
		if !ok {
			result.Error = fmt.Errorf("no data provided for expression: %s", field.Expressions[0])
			return result
		}
		err = r.setValueAtPath(field.Path, resolvedValue)
		if err != nil {
			result.Error = fmt.Errorf("error setting value: %w", err)
			return result
		}
		result.Resolved = true
		result.Replaced = resolvedValue
	} else {
		strValue, ok := value.(string)
		if !ok {
			result.Error = fmt.Errorf("expected string value for path %s", field.Path)
			return result
		}

		replaced := strValue
		for _, expr := range field.Expressions {
			replacement, ok := r.data[expr.String()]
			if !ok {
				result.Error = fmt.Errorf("no data provided for expression: %s", expr)
				return result
			}
			replaced = strings.ReplaceAll(replaced, "${"+expr.String()+"}", fmt.Sprintf("%v", replacement))
		}

		err = r.setValueAtPath(field.Path, replaced)
		if err != nil {
			result.Error = fmt.Errorf("error setting value: %w", err)
			return result
		}
		result.Resolved = true
		result.Replaced = replaced
	}

	return result
}

// getValueFromPath retrieves a value from the resource using a dot separated path.
// NOTE(a-hilaly): this is very similar to the `setValueAtPath` function maybe
// we can refactor something here.
// getValueFromPath retrieves a value from the resource using a dot-separated path.
func (r *Resolver) getValueFromPath(path fieldpath.Path) (interface{}, error) {
	current := interface{}(r.resource)
	for _, segment := range path {
		if segment.Index != nil {
			// Handle array access
			array, ok := current.([]interface{})
			if !ok {
				return nil, fmt.Errorf("expected array at path segment: %v", segment)
			}

			if *segment.Index >= len(array) {
				return nil, fmt.Errorf("array index out of bounds: %d", segment.Index)
			}

			current = array[*segment.Index]
		} else {
			// Handle object access
			currentMap, ok := current.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("expected map at path segment: %v", segment)
			}

			value, ok := currentMap[segment.Name]
			if !ok {
				return nil, fmt.Errorf("key not found: %s", segment.Name)
			}
			current = value
		}
	}

	return current, nil
}

func (r *Resolver) setValueAtPath(path fieldpath.Path, value interface{}) error {
	if len(path) == 0 {
		return nil
	}

	var current interface{} = r.resource
	var parent interface{}
	var parentKey string
	var parentIndex int
	var parentIsArray bool

	for i, segment := range path {
		last := i == len(path)-1

		if segment.Index != nil {
			// Ensure current is slice
			var arr []interface{}
			switch v := current.(type) {
			case []interface{}:
				arr = v
			case nil:
				arr = []interface{}{}
			default:
				return fmt.Errorf("expected array at %v", segment)
			}

			idx := *segment.Index

			// Reattach slice to parent
			var extended bool
			if arr, extended = ensureSliceLen(arr, idx); parent != nil && extended {
				if parentIsArray {
					parent.([]interface{})[parentIndex] = arr
				} else {
					parent.(map[string]interface{})[parentKey] = arr
				}
			}

			if last {
				arr[idx] = value
				return nil
			}

			// Prepare next level
			if arr[idx] == nil {
				if path[i+1].Index != nil {
					arr[idx] = []interface{}{}
				} else {
					arr[idx] = map[string]interface{}{}
				}
			}

			parent = arr
			parentIndex = idx
			parentIsArray = true
			current = arr[idx]
			continue
		}

		// Map segment
		m, ok := current.(map[string]interface{})
		if !ok {
			return fmt.Errorf("expected map at %v", segment)
		}

		if last {
			m[segment.Name] = value
			return nil
		}

		if m[segment.Name] == nil {
			if path[i+1].Index != nil {
				m[segment.Name] = []interface{}{}
			} else {
				m[segment.Name] = map[string]interface{}{}
			}
		}

		parent = m
		parentKey = segment.Name
		parentIsArray = false
		current = m[segment.Name]
	}

	return nil
}

func ensureSliceLen(s []interface{}, idx int) ([]interface{}, bool) {
	if idx < len(s) {
		return s, false
	}
	ns := make([]interface{}, idx+1)
	copy(ns, s)
	return ns, true
}
