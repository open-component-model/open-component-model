package credentialplugin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/credentials"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentialplugin/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// mockExternalCredentialPlugin is a test implementation of v1.CredentialPluginContract.
type mockExternalCredentialPlugin struct {
	getConsumerIdentityFunc func(ctx context.Context, req v1.GetConsumerIdentityRequest[runtime.Typed]) (runtime.Identity, error)
	resolveFunc             func(ctx context.Context, req v1.ResolveRequest[runtime.Typed], credentials map[string]string) (map[string]string, error)
	pingFunc                func(ctx context.Context) error
}

func (m *mockExternalCredentialPlugin) Ping(ctx context.Context) error {
	if m.pingFunc != nil {
		return m.pingFunc(ctx)
	}
	return nil
}

func (m *mockExternalCredentialPlugin) GetConsumerIdentity(ctx context.Context, req v1.GetConsumerIdentityRequest[runtime.Typed]) (runtime.Identity, error) {
	if m.getConsumerIdentityFunc != nil {
		return m.getConsumerIdentityFunc(ctx, req)
	}
	return runtime.Identity{"test": "identity"}, nil
}

func (m *mockExternalCredentialPlugin) Resolve(ctx context.Context, req v1.ResolveRequest[runtime.Typed], credentials map[string]string) (map[string]string, error) {
	if m.resolveFunc != nil {
		return m.resolveFunc(ctx, req, credentials)
	}
	return map[string]string{"resolved": "credentials"}, nil
}

func TestCredentialPluginConverter_GetConsumerIdentity(t *testing.T) {
	expectedIdentity := runtime.Identity{"test": "consumer"}
	mockCredential := &runtime.Unstructured{}
	mock := &mockExternalCredentialPlugin{
		getConsumerIdentityFunc: func(_ context.Context, req v1.GetConsumerIdentityRequest[runtime.Typed]) (runtime.Identity, error) {
			require.Equal(t, mockCredential, req.Credential)
			return expectedIdentity, nil
		},
	}

	converter := NewCredentialPluginConverter(mock)
	identity, err := converter.GetConsumerIdentity(t.Context(), mockCredential)
	require.NoError(t, err)
	require.Equal(t, expectedIdentity, identity)
}

func TestCredentialPluginConverter_Resolve(t *testing.T) {
	expectedCredentials := map[string]string{"username": "testuser", "password": "testpass"}
	mockIdentity := runtime.Identity{"consumer": "test"}
	inputCredentials := map[string]string{"existing": "cred"}
	mock := &mockExternalCredentialPlugin{
		resolveFunc: func(_ context.Context, req v1.ResolveRequest[runtime.Typed], creds map[string]string) (map[string]string, error) {
			require.Equal(t, mockIdentity, req.Identity)
			require.Equal(t, inputCredentials, creds)
			return expectedCredentials, nil
		},
	}

	converter := NewCredentialPluginConverter(mock)
	resolved, err := converter.Resolve(t.Context(), mockIdentity, inputCredentials)
	require.NoError(t, err)
	require.Equal(t, expectedCredentials, resolved)
}

func TestCredentialPluginConverter_Interface(t *testing.T) {
	var _ credentials.CredentialPlugin = NewCredentialPluginConverter(&mockExternalCredentialPlugin{})
}
