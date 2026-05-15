package credentialrepository

import (
	"context"
	"testing"

	"ocm.software/open-component-model/bindings/go/credentials"
	credconfigv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// mockExternalPlugin is a test implementation of v1.CredentialRepositoryPluginContract
type mockExternalPlugin struct {
	consumerIdentityForConfigFunc func(ctx context.Context, cfg v1.ConsumerIdentityForConfigRequest[runtime.Typed]) (runtime.Identity, error)
	resolveTypedFunc              func(ctx context.Context, cfg v1.ResolveRequest[runtime.Typed], credentials runtime.Typed) (runtime.Typed, error)
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

func (m *mockExternalPlugin) ResolveTyped(ctx context.Context, cfg v1.ResolveRequest[runtime.Typed], credentials runtime.Typed) (runtime.Typed, error) {
	if m.resolveTypedFunc != nil {
		return m.resolveTypedFunc(ctx, cfg, credentials)
	}
	return &credconfigv1.DirectCredentials{
		Type:       runtime.NewVersionedType(credconfigv1.CredentialsType, credconfigv1.Version),
		Properties: map[string]string{"resolved": "credentials"},
	}, nil
}

func TestCredentialRepositoryPluginConverter_ConsumerIdentityForConfig(t *testing.T) {
	expectedIdentity := runtime.Identity{"test": "consumer"}
	mockPlugin := &mockExternalPlugin{
		consumerIdentityForConfigFunc: func(ctx context.Context, cfg v1.ConsumerIdentityForConfigRequest[runtime.Typed]) (runtime.Identity, error) {
			return expectedIdentity, nil
		},
	}
	converter := NewCredentialRepositoryPluginConverter(mockPlugin)

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

func TestCredentialRepositoryPluginConverter_ResolveTyped(t *testing.T) {
	expected := &credconfigv1.DirectCredentials{
		Type:       runtime.NewVersionedType(credconfigv1.CredentialsType, credconfigv1.Version),
		Properties: map[string]string{"username": "testuser", "password": "testpass"},
	}
	mockPlugin := &mockExternalPlugin{
		resolveTypedFunc: func(ctx context.Context, cfg v1.ResolveRequest[runtime.Typed], credentials runtime.Typed) (runtime.Typed, error) {
			return expected, nil
		},
	}
	converter := NewCredentialRepositoryPluginConverter(mockPlugin)

	mockConfig := &runtime.Unstructured{}
	mockIdentity := runtime.Identity{"consumer": "test"}

	resolved, err := converter.ResolveTyped(context.Background(), mockConfig, mockIdentity, nil)
	if err != nil {
		t.Errorf("ResolveTyped() returned unexpected error: %v", err)
	}

	dc, ok := resolved.(*credconfigv1.DirectCredentials)
	if !ok {
		t.Fatalf("ResolveTyped() returned %T, expected *DirectCredentials", resolved)
	}

	for key, value := range expected.Properties {
		if dc.Properties[key] != value {
			t.Errorf("ResolveTyped() returned credentials[%s] = %s, expected %s", key, dc.Properties[key], value)
		}
	}
}

func TestCredentialRepositoryPluginConverter_Interface(t *testing.T) {
	mockPlugin := &mockExternalPlugin{}
	converter := NewCredentialRepositoryPluginConverter(mockPlugin)

	var _ credentials.RepositoryPlugin = converter
}
