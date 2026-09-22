package graph

import (
	"fmt"

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

// DisplayName returns the human-readable name of the transformation:
// The Label when set, else the hash-based "ID [Type]" form.
func (t *Transformation) DisplayName() string {
	if t.Label != "" {
		return t.Label
	}
	return fmt.Sprintf("%s [%s]", t.ID, t.Type.Name)
}
