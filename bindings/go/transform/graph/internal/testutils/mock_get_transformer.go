package testutils

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/runtime"
)

type MockGetObject struct {
	Scheme *runtime.Scheme
}

func (t *MockGetObject) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	transformation := &MockGetObjectTransformer{}
	if err := t.Scheme.Convert(step, transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to download component transformation: %v", err)
	}
	transformation.Output = &MockGetObjectTransformerOutput{
		Object: MockObject{
			Name:    transformation.Spec.Name,
			Version: transformation.Spec.Version,
			Content: fmt.Sprintf("object retrieved by %s at step with id %s", MockGetObjectV1alpha1, transformation.ID),
		},
	}
	return transformation, nil
}
