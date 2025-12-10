package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/cel-go/cel"
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

type Runtime struct {
	Environment              *cel.Env
	transformations          map[string]graph.Transformation
	EvaluatedExpressionCache map[string]any
	EvaluatedTransformations map[string]any

	scheme       *runtime.Scheme
	Transformers map[runtime.Type]Transformer
}

func (b *Runtime) ProcessValue(ctx context.Context, transformation graph.Transformation) error {
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

	runtimeType := transformation.GenericTransformation.GetType()
	if runtimeType.IsEmpty() {
		return fmt.Errorf("transformation type after render is empty")
	}

	transformer, ok := b.Transformers[runtimeType]
	if !ok {
		return fmt.Errorf("no transformer runtime registered for type %s", runtimeType)
	}

	transformed, err := transformer.Transform(ctx, transformation.GenericTransformation.AsRaw())
	if err != nil {
		return fmt.Errorf("failed to transform transformation %q: %w", transformation.ID, err)
	}
	updated, err := v1alpha1.GenericTransformationFromTyped(transformed)
	if err != nil {
		return fmt.Errorf("failed to convert updated transformation %q to generic transformation: %w", transformation.ID, err)
	}
	evaluatedTransformation := updated.AsUnstructured().Data

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
