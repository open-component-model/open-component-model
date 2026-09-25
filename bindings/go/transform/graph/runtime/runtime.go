package runtime

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"

	"cel.dev/cel-go/cel"
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

// Runtime evaluates every transformation in the graph. It is safe for
// concurrent use: EvaluatedExpressionCache and EvaluatedTransformations are
// guarded by mu. Reads (including CEL evaluation, which reads the shared
// EvaluatedTransformations activation) take a read lock, while the single
// write per node takes a write lock. The network-bound Transformer.Transform
// call runs without holding any lock so independent nodes transfer in parallel.
type Runtime struct {
	Environment              *cel.Env
	EvaluatedExpressionCache map[string]any
	EvaluatedTransformations map[string]any

	Transformers map[runtime.Type]Transformer
	Events       chan<- ProgressEvent

	mu sync.RWMutex
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
			b.mu.RLock()
			_, found := b.EvaluatedExpressionCache[expression.String()]
			b.mu.RUnlock()
			if found {
				continue
			}
			program, err := b.Environment.Program(expression.AST)
			if err != nil {
				return fmt.Errorf("failed to create program for expression %q: %w", expression.String(), err)
			}
			// The activation reads predecessor results, which are final by the
			// time this node runs. The read lock only guards against concurrent
			// writes from sibling nodes storing their own results.
			b.mu.RLock()
			result, _, err := program.Eval(b.EvaluatedTransformations)
			b.mu.RUnlock()
			if err != nil {
				return fmt.Errorf("failed to evaluate expression %q: %w", expression.String(), err)
			}

			val, err := GoNativeValue(result)
			if err != nil {
				return fmt.Errorf("failed to convert result of expression %q to go native type: %w", expression.String(), err)
			}
			b.mu.Lock()
			b.EvaluatedExpressionCache[expression.String()] = val
			b.mu.Unlock()
		}
	}
	// Snapshot the cache so the resolver reads a stable view while sibling
	// nodes may still be writing their own expression results.
	b.mu.RLock()
	expressionData := maps.Clone(b.EvaluatedExpressionCache)
	b.mu.RUnlock()
	res := resolver.NewResolver(transformation.Spec.Data, expressionData, specSubSchema(transformation.Schema))
	summary := res.Resolve(transformation.FieldDescriptors)
	if len(summary.Errors) > 0 {
		return fmt.Errorf("failed to resolve transformation %q: %w", transformation.DisplayName(), errors.Join(summary.Errors...))
	}

	unstructuredTransformationData := transformation.GenericTransformation.AsUnstructured().Data
	fieldDescriptors, err := stv6jsonschema.ParseResource(
		unstructuredTransformationData,
		transformation.Schema,
	)
	if err != nil {
		return fmt.Errorf("failed to parse resolved transformation %q: %w", transformation.DisplayName(), err)
	}
	if len(fieldDescriptors) > 0 {
		return fmt.Errorf("transformation %q has unresolved fields after resolution", transformation.DisplayName())
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
		return fmt.Errorf("failed to transform transformation %q: %w", transformation.DisplayName(), err)
	}
	updated, err := v1alpha1.GenericTransformationFromTyped(transformed)
	if err != nil {
		return fmt.Errorf("failed to convert updated transformation %q to generic transformation: %w", transformation.DisplayName(), err)
	}
	evaluatedTransformation := updated.AsUnstructured().Data

	fieldDescriptors, err = stv6jsonschema.ParseResource(
		evaluatedTransformation,
		transformation.Schema,
	)
	if err != nil {
		return fmt.Errorf("failed to parse evaluated transformation %q: %w", transformation.DisplayName(), err)
	}
	if len(fieldDescriptors) > 0 {
		return fmt.Errorf("transformation %q has unresolved fields after evaluation", transformation.DisplayName())
	}

	b.mu.Lock()
	b.EvaluatedTransformations[transformation.ID] = evaluatedTransformation
	b.mu.Unlock()
	return nil
}

// specSubSchema extracts the "spec" sub-schema from a full transformation
// schema. The resolver works with Spec.Data (the contents of the spec field),
// so the schema passed to it must match that level. Returns nil if the spec
// property is not found.
func specSubSchema(schema *jsonschema.Schema) *jsonschema.Schema {
	if schema == nil || schema.Properties == nil {
		return nil
	}
	sp := schema.Properties["spec"]
	if sp == nil {
		return nil
	}
	if sp.Ref != nil {
		return sp.Ref
	}
	return sp
}
