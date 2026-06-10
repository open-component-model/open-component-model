package testutils

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// MockCredentialAware implements both Transformer and TransformerWithCredentials.
// Tests configure Identities to control which slots are declared, and read
// ReceivedCreds after execution to assert what the runtime passed in.
type MockCredentialAware struct {
	Scheme      *runtime.Scheme
	Identities  map[string]runtime.Identity
	IdentityErr error

	ReceivedCreds map[string]runtime.Typed
}

// Transform satisfies the Transformer interface; the runtime takes the
// TransformerWithCredentials path instead when both are implemented.
func (m *MockCredentialAware) Transform(_ context.Context, _ runtime.Typed) (runtime.Typed, error) {
	panic("Transform called on MockCredentialAware — expected TransformWithCredentials")
}

func (m *MockCredentialAware) GetCredentialConsumerIdentities(_ context.Context, _ runtime.Typed) (map[string]runtime.Identity, error) {
	if m.IdentityErr != nil {
		return nil, m.IdentityErr
	}
	return m.Identities, nil
}

func (m *MockCredentialAware) TransformWithCredentials(_ context.Context, step runtime.Typed, creds map[string]runtime.Typed) (runtime.Typed, error) {
	m.ReceivedCreds = creds
	transformation := &MockCredentialAwareTransformer{}
	if err := m.Scheme.Convert(step, transformation); err != nil {
		return nil, fmt.Errorf("failed converting step to MockCredentialAwareTransformer: %w", err)
	}
	transformation.Output = &MockCredentialAwareTransformerOutput{
		Object: MockObject{
			Name:    transformation.Spec.Name,
			Version: transformation.Spec.Version,
			Content: fmt.Sprintf("object transformed by %s at step with id %s", MockCredentialAwareV1alpha1, transformation.ID),
		},
	}
	return transformation, nil
}
