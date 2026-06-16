package credentialplugin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentialplugin/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var pluginDummyType = runtime.NewVersionedType(dummyv1.Type, dummyv1.Version)

func newDummyCapability(schema []byte) v1.CapabilitySpec {
	return v1.CapabilitySpec{
		Type: runtime.NewUnversionedType(string(v1.CredentialPluginType)),
		SupportedCredentialPluginTypes: []types.Type{{
			Type:       pluginDummyType,
			JSONSchema: schema,
		}},
	}
}

func newPluginConfig() types.Config {
	return types.Config{
		ID:         "test-plugin",
		Type:       types.TCP,
		PluginType: v1.CredentialPluginType,
	}
}

func TestGetConsumerIdentity(t *testing.T) {
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)

	tests := []struct {
		name       string
		request    v1.GetConsumerIdentityRequest[runtime.Typed]
		setupMock  func() *httptest.Server
		expectErr  bool
		expectedID string
	}{
		{
			name: "success",
			request: v1.GetConsumerIdentityRequest[runtime.Typed]{
				Credential: &runtime.Raw{Type: pluginDummyType, Data: []byte(`{}`)},
			},
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == GetConsumerIdentityEndpoint {
						require.NoError(t, json.NewEncoder(w).Encode(map[string]string{"id": "test-identity", "type": pluginDummyType.String()}))
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			expectedID: "test-identity",
		},
		{
			name: "validation_failed",
			request: v1.GetConsumerIdentityRequest[runtime.Typed]{
				Credential: &dummyv1.Repository{},
			},
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
			},
			expectErr: true,
		},
		{
			name: "call_failed",
			request: v1.GetConsumerIdentityRequest[runtime.Typed]{
				Credential: &runtime.Raw{Type: pluginDummyType, Data: []byte(`{}`)},
			},
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.setupMock()
			defer server.Close()

			plugin := NewCredentialPlugin(server.Client(), "test-plugin", server.URL, newPluginConfig(), server.URL, newDummyCapability([]byte(`{}`)))
			identity, err := plugin.GetConsumerIdentity(t.Context(), tt.request)
			if tt.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.expectedID, identity["id"])
		})
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name        string
		request     v1.ResolveRequest[runtime.Typed]
		credentials map[string]string
		setupMock   func() *httptest.Server
		expectErr   bool
		expectedKey string
	}{
		{
			name: "success",
			request: v1.ResolveRequest[runtime.Typed]{
				Identity: map[string]string{"id": "test-identity"},
			},
			credentials: map[string]string{"key": "value"},
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == ResolveEndpoint {
						require.NoError(t, json.NewEncoder(w).Encode(map[string]string{"resolved": "credentials", "token": "abc123"}))
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			expectedKey: "abc123",
		},
		{
			name: "invalid_credentials",
			request: v1.ResolveRequest[runtime.Typed]{
				Identity: map[string]string{"id": "test-identity"},
			},
			credentials: map[string]string{"invalid_key": "invalid_value"},
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				}))
			},
			expectErr: true,
		},
		{
			name: "call_failed",
			request: v1.ResolveRequest[runtime.Typed]{
				Identity: map[string]string{"id": "test-identity"},
			},
			credentials: map[string]string{"key": "value"},
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.setupMock()
			defer server.Close()

			plugin := NewCredentialPlugin(server.Client(), "test-plugin", server.URL, newPluginConfig(), server.URL, newDummyCapability([]byte(`{}`)))
			resolved, err := plugin.Resolve(t.Context(), tt.request, tt.credentials)
			if tt.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.expectedKey, resolved["token"])
		})
	}
}

func TestPing(t *testing.T) {
	tests := []struct {
		name      string
		setupMock func() *httptest.Server
		expectErr bool
	}{
		{
			name: "success",
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/healthz" {
						w.WriteHeader(http.StatusOK)
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))
			},
		},
		{
			name: "failed_ping",
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.setupMock()
			defer server.Close()

			plugin := NewCredentialPlugin(server.Client(), "test-plugin", server.URL, newPluginConfig(), server.URL, newDummyCapability([]byte(`{}`)))
			err := plugin.Ping(t.Context())
			if tt.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestToCredentials(t *testing.T) {
	tests := []struct {
		name        string
		credentials map[string]string
	}{
		{name: "valid", credentials: map[string]string{"key": "value"}},
		{name: "empty", credentials: map[string]string{}},
		{name: "multiple_keys", credentials: map[string]string{"key1": "value1", "key2": "value2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kv, err := toCredentials(tt.credentials)
			require.NoError(t, err)
			require.Equal(t, "Authorization", kv.Key)
			require.NotEmpty(t, kv.Value)
		})
	}
}
