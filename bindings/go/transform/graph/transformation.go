package graph

import (
	"github.com/santhosh-tekuri/jsonschema/v6"

	inspector "ocm.software/open-component-model/bindings/go/cel/expression/inspector"
	"ocm.software/open-component-model/bindings/go/cel/expression/variable"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

type Transformation struct {
	v1alpha1.GenericTransformation
	FieldDescriptors []variable.FieldDescriptor
	Expressions      []inspector.ExpressionInspection
	Schema           *jsonschema.Schema
}
