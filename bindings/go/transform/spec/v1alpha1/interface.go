package v1alpha1

import (
	"context"

	"ocm.software/open-component-model/bindings/go/cel/jsonschema/decl"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

type Transformation interface {
	GetTransformationMeta() *meta.TransformationMeta
	NestedTypedFields() []string
	NewDeclType(nestedTypedFields map[string]runtime.Type) (*decl.Type, error)
	FromGeneric(generic *GenericTransformation) error
	Transform(ctx context.Context, credentialProvider credentials.Resolver) (map[string]any, error)
}

type Transformer interface {
	Transform(transformation Transformation)
}
