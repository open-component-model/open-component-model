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

type Transformer interface {
	// GetCredentialConsumerIdentities returns credential identities this transformer needs for the given step.
	// Map key is a transformer-defined slot name (e.g. "repository", "resource").
	// Multi-credential transformers may use keys like "source" and "target" for both ends of a transfer.
	// Returns nil or empty map when no credentials are needed.
	GetCredentialConsumerIdentities(ctx context.Context, step runtime.Typed) (map[string]runtime.Identity, error)

	// Transform executes the transformation with resolved credentials.
	//
	// The credentials parameter follows these semantics:
	//   - nil: no credentials were requested (GetCredentialConsumerIdentities returned nil/empty)
	//     or no credential resolver is configured.
	//   - non-nil map with nil value for a slot key: the resolver was present and the identity
	//     was looked up, but no matching credentials were found (ErrNotFound was swallowed).
	//     Implementations should treat a nil inner map the same as missing credentials.
	Transform(ctx context.Context, step runtime.Typed, credentials map[string]map[string]string) (runtime.Typed, error)
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

	Transformers       map[runtime.Type]Transformer
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

	identities, err := transformer.GetCredentialConsumerIdentities(ctx, step)
	if err != nil {
		return fmt.Errorf("failed to get credential consumer identities for transformation %q: %w", transformation.ID, err)
	}

	var creds map[string]map[string]string
	if len(identities) > 0 && b.CredentialProvider != nil {
		creds = make(map[string]map[string]string, len(identities))
		for name, identity := range identities {
			resolved, err := b.CredentialProvider.Resolve(ctx, identity)
			if err != nil && !errors.Is(err, credentials.ErrNotFound) {
				return fmt.Errorf("failed to resolve credentials %q for transformation %q: %w", name, transformation.ID, err)
			}
			creds[name] = resolved
		}
	}

	transformed, err := transformer.Transform(ctx, step, creds)
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
