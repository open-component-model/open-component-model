package testutils

import (
	"context"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// MockCredentialResolver resolves identities from a fixed map.
// Identities not present in Entries return credentials.ErrNotFound.
// If Err is set it is returned for every Resolve call.
type MockCredentialResolver struct {
	Entries map[string]runtime.Typed
	Err     error
}

func (r *MockCredentialResolver) Resolve(_ context.Context, identity runtime.Identity) (runtime.Typed, error) {
	if r.Err != nil {
		return nil, r.Err
	}
	if cred, ok := r.Entries[identity.String()]; ok {
		return cred, nil
	}
	return nil, credentials.ErrNotFound
}

// MockTypedCredential is a minimal runtime.Typed used as a stand-in credential in tests.
type MockTypedCredential struct {
	ID string
}

func (m *MockTypedCredential) GetType() runtime.Type        { return runtime.NewVersionedType("MockCredential", "v1") }
func (m *MockTypedCredential) SetType(_ runtime.Type)       {}
func (m *MockTypedCredential) DeepCopyTyped() runtime.Typed { return &MockTypedCredential{ID: m.ID} }
