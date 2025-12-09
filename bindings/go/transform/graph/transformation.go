package graph

import (
	inspector "ocm.software/open-component-model/bindings/go/cel/expression/inspector"
	"ocm.software/open-component-model/bindings/go/cel/expression/variable"
	stv6jsonschema "ocm.software/open-component-model/bindings/go/cel/jsonschema/santhosh-tekuri/v6"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

type Transformation struct {
	v1alpha1.GenericTransformation
	FieldDescriptors []variable.FieldDescriptor
	Expressions      []inspector.ExpressionInspection
	Order            int
	DeclType         *stv6jsonschema.DeclType
}
