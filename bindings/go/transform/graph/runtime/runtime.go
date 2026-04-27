package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/cel-go/cel"
	"github.com/santhosh-tekuri/jsonschema/v6"

	stv6jsonschema "ocm.software/open-component-model/bindings/go/cel/jsonschema/santhosh-tekuri/v6"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/graph"
	"ocm.software/open-component-model/bindings/go/transform/graph/runtime/resolver"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

// Transformer is the core interface for transformation graph nodes.
type Transformer interface {
	Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error)
}

// TransformerWithCredentials is an optional interface. Transformers that need credentials
// implement this to declare their identities. The graph resolves them before calling Transform.
// Transformers that don't need credentials simply don't implement this interface.
type TransformerWithCredentials interface {
	Transform(ctx context.Context, step runtime.Typed, credentials map[string]map[string]string) (runtime.Typed, error)
	GetCredentialConsumerIdentities(ctx context.Context, step runtime.Typed) (map[string]runtime.Identity, error)
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

	Transformers       map[runtime.Type]any
	CredentialProvider credentials.Resolver
	Events             chan<- ProgressEvent
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
	res := resolver.NewResolver(transformation.Spec.Data, b.EvaluatedExpressionCache, specSubSchema(transformation.Schema))
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

	runtimeType := transformation.GetType()
	if runtimeType.IsEmpty() {
		return fmt.Errorf("transformation type after render is empty")
	}

	transformer, ok := b.Transformers[runtimeType]
	if !ok {
		return fmt.Errorf("no transformer runtime registered for type %s", runtimeType)
	}

	step := transformation.AsRaw()

	var transformed runtime.Typed
	if twc, ok := transformer.(TransformerWithCredentials); ok {
		identities, identityErr := twc.GetCredentialConsumerIdentities(ctx, step)
		if identityErr != nil {
			return fmt.Errorf("failed to get credential consumer identities for transformation %q: %w", transformation.ID, identityErr)
		}

		var creds map[string]map[string]string
		if len(identities) > 0 && b.CredentialProvider != nil {
			creds = make(map[string]map[string]string, len(identities))
			for name, identity := range identities {
				resolved, resolveErr := b.CredentialProvider.Resolve(ctx, identity)
				if resolveErr != nil && !errors.Is(resolveErr, credentials.ErrNotFound) {
					return fmt.Errorf("failed to resolve credentials %q for transformation %q: %w", name, transformation.ID, resolveErr)
				}
				creds[name] = resolved
			}
		}

		var transformErr error
		transformed, transformErr = twc.Transform(ctx, step, creds)
		if transformErr != nil {
			return fmt.Errorf("failed to execute transformation %q: %w", transformation.ID, transformErr)
		}
	} else if t, ok := transformer.(Transformer); ok {
		var transformErr error
		transformed, transformErr = t.Transform(ctx, step)
		if transformErr != nil {
			return fmt.Errorf("failed to execute transformation %q: %w", transformation.ID, transformErr)
		}
	} else {
		return fmt.Errorf("transformer for type %s implements neither Transformer nor TransformerWithCredentials", runtimeType)
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
