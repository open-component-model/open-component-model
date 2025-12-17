package testutils

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/runtime"
)

type MockCustomSchema struct {
	Scheme *runtime.Scheme
}

func (t *MockCustomSchema) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	transformation := &MockCustomSchemaObjectTransformer{}
	if err := t.Scheme.Convert(step, transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to mock add object transformation: %w", err)
	}

	transformation.Output = &MockCustomSchemaObjectTransformerOutput{
		String: fmt.Sprintf("object transformed by %s at step with id %s", MockCustomSchemaObjectV1alpha1, transformation.ID),
	}
	return transformation, nil
}
