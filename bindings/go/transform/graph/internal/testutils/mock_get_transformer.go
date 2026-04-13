package testutils

import (
	"context"
	"fmt"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type MockGetObject struct {
	Scheme *runtime.Scheme
}

func (t *MockGetObject) GetCredentialConsumerIdentities(_ context.Context, _ runtime.Typed) (map[string]runtime.Identity, error) {
	return nil, nil
}

func (t *MockGetObject) Transform(ctx context.Context, step runtime.Typed, _ map[string]map[string]string) (runtime.Typed, error) {
	transformation := &MockGetObjectTransformer{}
	if err := t.Scheme.Convert(step, transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to mock get object transformation: %w", err)
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
