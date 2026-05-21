package credentialrepository

import (
	"context"
	"testing"

	"ocm.software/open-component-model/bindings/go/credentials"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// mockExternalPlugin is a test implementation of v1.CredentialRepositoryPluginContract
type mockExternalPlugin struct {
	consumerIdentityForConfigFunc func(ctx context.Context, cfg v1.ConsumerIdentityForConfigRequest[runtime.Typed]) (runtime.Identity, error)
	resolveFunc                   func(ctx context.Context, cfg v1.ResolveRequest[runtime.Typed], credentials runtime.Typed) (runtime.Typed, error)
	pingFunc                      func(ctx context.Context) error
}

func (m *mockExternalPlugin) Ping(ctx context.Context) error {
	if m.pingFunc != nil {
		return m.pingFunc(ctx)
	}
	return nil
}

func (m *mockExternalPlugin) ConsumerIdentityForConfig(ctx context.Context, cfg v1.ConsumerIdentityForConfigRequest[runtime.Typed]) (runtime.Identity, error) {
	if m.consumerIdentityForConfigFunc != nil {
		return m.consumerIdentityForConfigFunc(ctx, cfg)
	}
	return runtime.Identity{"test": "identity"}, nil
}

func (m *mockExternalPlugin) Resolve(ctx context.Context, cfg v1.ResolveRequest[runtime.Typed], credentials runtime.Typed) (runtime.Typed, error) {
	if m.resolveFunc != nil {
		return m.resolveFunc(ctx, cfg, credentials)
	}
	return runtime.Identity{"resolved": "credentials"}, nil
}

func TestCredentialRepositoryPluginConverter_ConsumerIdentityForConfig(t *testing.T) {
	expectedIdentity := runtime.Identity{"test": "consumer"}
	mockPlugin := &mockExternalPlugin{
		consumerIdentityForConfigFunc: func(ctx context.Context, cfg v1.ConsumerIdentityForConfigRequest[runtime.Typed]) (runtime.Identity, error) {
			return expectedIdentity, nil
		},
	}
	converter := NewCredentialRepositoryPluginConverter(mockPlugin)

	// Create a mock typed config
	mockConfig := &runtime.Unstructured{}

	identity, err := converter.ConsumerIdentityForConfig(context.Background(), mockConfig)
	if err != nil {
		t.Errorf("ConsumerIdentityForConfig() returned unexpected error: %v", err)
	}

	if len(identity) != len(expectedIdentity) {
		t.Errorf("ConsumerIdentityForConfig() returned identity with length %d, expected %d", len(identity), len(expectedIdentity))
	}

	for key, value := range expectedIdentity {
		if identity[key] != value {
			t.Errorf("ConsumerIdentityForConfig() returned identity[%s] = %s, expected %s", key, identity[key], value)
		}
	}
}

func TestCredentialRepositoryPluginConverter_Resolve(t *testing.T) {
	expectedCredentials := runtime.Identity{"username": "testuser", "password": "testpass"}
	mockPlugin := &mockExternalPlugin{
		resolveFunc: func(ctx context.Context, cfg v1.ResolveRequest[runtime.Typed], credentials runtime.Typed) (runtime.Typed, error) {
			return expectedCredentials, nil
		},
	}
	converter := NewCredentialRepositoryPluginConverter(mockPlugin)

	// Create a mock typed config and identity
	mockConfig := &runtime.Unstructured{}
	mockIdentity := runtime.Identity{"consumer": "test"}
	inputCredentials := runtime.Identity{"existing": "cred"}

	resolvedTyped, err := converter.Resolve(context.Background(), mockConfig, mockIdentity, inputCredentials)
	if err != nil {
		t.Errorf("Resolve() returned unexpected error: %v", err)
	}

	resolvedCredentials, ok := resolvedTyped.(runtime.Identity)
	if !ok {
		t.Fatalf("Resolve() returned credentials of type %T, expected runtime.Identity", resolvedTyped)
	}

	if len(resolvedCredentials) != len(expectedCredentials) {
		t.Errorf("Resolve() returned credentials with length %d, expected %d", len(resolvedCredentials), len(expectedCredentials))
	}

	for key, value := range expectedCredentials {
		if resolvedCredentials[key] != value {
			t.Errorf("Resolve() returned credentials[%s] = %s, expected %s", key, resolvedCredentials[key], value)
		}
	}
}

func TestCredentialRepositoryPluginConverter_Interface(t *testing.T) {
	mockPlugin := &mockExternalPlugin{}
	converter := NewCredentialRepositoryPluginConverter(mockPlugin)

	// Verify that the converter implements the correct interface
	var _ credentials.RepositoryPlugin = converter
}