package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/cel-go/cel"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/graph"
	"ocm.software/open-component-model/bindings/go/transform/graph/runtime/resolver"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

type Transformer interface {
	Transform(
		ctx context.Context,
		step *v1alpha1.GenericTransformation,
	) (*v1alpha1.GenericTransformation, error)
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

			// TODO maybe consider dropping this for a smarter solution
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

	runtimeType := transformation.GenericTransformation.GetType()
	if runtimeType.IsEmpty() {
		return fmt.Errorf("transformation type after render is empty")
	}

	transformer, ok := b.Transformers[runtimeType]
	if !ok {
		return fmt.Errorf("no transformer runtime registered for type %s", runtimeType)
	}

	output, err := transformer.Transform(ctx, &transformation.GenericTransformation)
	if err != nil {
		return fmt.Errorf("failed to transform transformation %q: %w", transformation.ID, err)
	}
	evaluatedTransformation := map[string]any{
		"spec":   transformation.Spec.Data,
		"output": output,
	}
	b.EvaluatedTransformations[transformation.ID] = evaluatedTransformation
	return nil
}
