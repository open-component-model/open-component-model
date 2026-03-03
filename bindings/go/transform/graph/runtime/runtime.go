package runtime

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/cel-go/cel"
	"github.com/santhosh-tekuri/jsonschema/v6"

	stv6jsonschema "ocm.software/open-component-model/bindings/go/cel/jsonschema/santhosh-tekuri/v6"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/graph"
	"ocm.software/open-component-model/bindings/go/transform/graph/runtime/resolver"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

type Transformer interface {
	Transform(
		ctx context.Context,
		step runtime.Typed,
	) (runtime.Typed, error)
}

// State represents the state of a transformation node.
type State int

func (s State) String() string {
	switch s {
	case Running:
		return "running"
	case Completed:
		return "completed"
	case Failed:
		return "failed"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}

const (
	// Running means the transformation is currently being processed.
	Running State = iota
	// Completed means the transformation completed successfully.
	Completed
	// Failed means the transformation failed.
	Failed
)

// ProgressEvent represents a state change during graph execution.
type ProgressEvent struct {
	Transformation *graph.Transformation
	State          State
	Err            error
}

type Runtime struct {
	Environment              *cel.Env
	EvaluatedExpressionCache map[string]any
	EvaluatedTransformations map[string]any

	Transformers map[runtime.Type]Transformer
	Events       chan<- ProgressEvent
}

func (b *Runtime) ProcessValue(ctx context.Context, transformation graph.Transformation) error {
	t := &transformation
	if b.Events != nil {
		b.Events <- ProgressEvent{Transformation: t, State: Running}
	}
	if err := b.processTransformation(ctx, transformation); err != nil {
		if b.Events != nil {
			b.Events <- ProgressEvent{Transformation: t, State: Failed, Err: err}
		}
		return err
	}

	if b.Events != nil {
		b.Events <- ProgressEvent{Transformation: t, State: Completed}
	}
	return nil
}

func (b *Runtime) processTransformation(ctx context.Context, transformation graph.Transformation) error {
	for _, fieldDescriptor := range transformation.FieldDescriptors {
		for _, expression := range fieldDescriptor.Expressions {
			if _, found := b.EvaluatedExpressionCache[expression.String()]; found {
				continue
			}
			program, err := b.Environment.Program(expression.AST)
			if err != nil {
				return fmt.Errorf("failed to create program for expression %q: %w", expression.String(), err)
			}
			result, _, err := program.Eval(b.EvaluatedTransformations)
			if err != nil {
				return fmt.Errorf("failed to evaluate expression %q: %w", expression.String(), err)
			}

			val, err := GoNativeValue(result)
			if err != nil {
				return fmt.Errorf("failed to convert result of expression %q to go native type: %w", expression.String(), err)
			}
			b.EvaluatedExpressionCache[expression.String()] = val
		}
	}
	res := resolver.NewResolver(transformation.Spec.Data, b.EvaluatedExpressionCache)
	summary := res.Resolve(transformation.FieldDescriptors)
	if len(summary.Errors) > 0 {
		return fmt.Errorf("failed to resolve transformation %q: %w", transformation.ID, errors.Join(summary.Errors...))
	}

	unstructuredTransformationData := transformation.GenericTransformation.AsUnstructured().Data
	stripNullPointerValues(unstructuredTransformationData, transformation.Schema)
	fieldDescriptors, err := stv6jsonschema.ParseResource(
		unstructuredTransformationData,
		transformation.Schema,
	)
	if err != nil {
		return fmt.Errorf("failed to parse resolved transformation %q: %w", transformation.ID, err)
	}
	if len(fieldDescriptors) > 0 {
		return fmt.Errorf("transformation %q has unresolved fields after resolution", transformation.ID)
	}

	runtimeType := transformation.GetType()
	if runtimeType.IsEmpty() {
		return fmt.Errorf("transformation type after render is empty")
	}

	transformer, ok := b.Transformers[runtimeType]
	if !ok {
		return fmt.Errorf("no transformer runtime registered for type %s", runtimeType)
	}

	transformed, err := transformer.Transform(ctx, transformation.AsRaw())
	if err != nil {
		return fmt.Errorf("failed to transform transformation %q: %w", transformation.ID, err)
	}
	updated, err := v1alpha1.GenericTransformationFromTyped(transformed)
	if err != nil {
		return fmt.Errorf("failed to convert updated transformation %q to generic transformation: %w", transformation.ID, err)
	}
	evaluatedTransformation := updated.AsUnstructured().Data
	stripNullPointerValues(evaluatedTransformation, transformation.Schema)

	fieldDescriptors, err = stv6jsonschema.ParseResource(
		evaluatedTransformation,
		transformation.Schema,
	)
	if err != nil {
		return fmt.Errorf("failed to parse evaluated transformation %q: %w", transformation.ID, err)
	}
	if len(fieldDescriptors) > 0 {
		return fmt.Errorf("transformation %q has unresolved fields after evaluation", transformation.ID)
	}

	b.EvaluatedTransformations[transformation.ID] = evaluatedTransformation
	return nil
}

// stripNullPointerValues recursively removes nil entries from maps where the
// corresponding schema property represents a pointer type with omitempty.
// At the JSON-Schema level this means the property is NOT in the required
// array (omitempty) and its schema is a $ref (Go struct pointer).
func stripNullPointerValues(m map[string]any, schema *jsonschema.Schema) {
	if schema == nil {
		return
	}
	for key, val := range m {
		propSchema := schemaForProperty(schema, key)

		if val == nil {
			if propSchema != nil && !slices.Contains(schema.Required, key) && isRefSchema(propSchema) {
				delete(m, key)
			}
			continue
		}
		if nested, ok := val.(map[string]any); ok && propSchema != nil {
			stripNullPointerValues(nested, resolveSchema(propSchema))
		}
	}
}

// schemaForProperty returns the sub-schema for the named property, or nil.
func schemaForProperty(schema *jsonschema.Schema, name string) *jsonschema.Schema {
	if schema == nil || schema.Properties == nil {
		return nil
	}
	return schema.Properties[name]
}

// isRefSchema returns true if the schema is a $ref to another type,
// which corresponds to a Go pointer-to-struct field.
func isRefSchema(s *jsonschema.Schema) bool {
	return s != nil && s.Ref != nil
}

// resolveSchema follows a $ref if present, returning the referenced schema.
func resolveSchema(s *jsonschema.Schema) *jsonschema.Schema {
	if s != nil && s.Ref != nil {
		return s.Ref
	}
	return s
}
