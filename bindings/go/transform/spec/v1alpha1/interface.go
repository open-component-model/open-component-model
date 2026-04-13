package v1alpha1

import (
	"context"

	"ocm.software/open-component-model/bindings/go/cel/jsonschema/decl"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// Transformation is the spec-level interface for a typed transformation object.
// Unlike runtime.Transformer (which receives the step as a generic runtime.Typed parameter),
// Transformation methods operate on the concrete object itself — the receiver already holds
// its own data, so no step parameter is needed.
type Transformation interface {
	GetTransformationMeta() *meta.TransformationMeta
	NestedTypedFields() []string
	NewDeclType(nestedTypedFields map[string]runtime.Type) (*decl.Type, error)
	FromGeneric(generic *GenericTransformation) error
	GetCredentialConsumerIdentities(ctx context.Context) (map[string]runtime.Identity, error)
	Transform(ctx context.Context, credentials map[string]map[string]string) (map[string]any, error)
}

type Transformer interface {
	Transform(transformation Transformation)
}
