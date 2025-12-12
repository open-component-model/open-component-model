package testutils

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/runtime"
)

type MockAddObject struct {
	Scheme *runtime.Scheme
}

func (t *MockAddObject) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	transformation := &MockAddObjectTransformer{}
	if err := t.Scheme.Convert(step, transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to mock add object transformation: %w", err)
	}
	transformation.Output = &MockAddObjectTransformerOutput{
		Object: MockObject{
			Name:    transformation.Spec.Object.Name,
			Version: transformation.Spec.Object.Version,
			Content: fmt.Sprintf("object added by %s at step with id %s", MockAddObjectV1alpha1, transformation.ID),
		},
	}
	return transformation, nil
}
