package credentialrepository

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentials/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var (
	dummyType = runtime.NewVersionedType(dummyv1.Type, dummyv1.Version)
)

func dummyCapability(schema []byte) v1.CapabilitySpec {
	return v1.CapabilitySpec{
		Type: runtime.NewUnversionedType(string(v1.CredentialRepositoryPluginType)),
		SupportedConsumerIdentityTypes: []types.Type{{
			Type:       dummyType,
			JSONSchema: schema,
		}},
	}
}

func TestConsumerIdentityForConfig(t *testing.T) {
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)

	tests := []struct {
		name       string
		request    v1.ConsumerIdentityForConfigRequest[runtime.Typed]
		setupMock  func() *httptest.Server
		expectErr  bool
		expectedID string
	}{
		{
			name: "success",
			request: v1.ConsumerIdentityForConfigRequest[runtime.Typed]{
				Config: &runtime.Raw{
					Type: dummyType,
					Data: []byte(`{}`),
				},
			},
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == ConsumerIdentityForConfig {
						identity := map[string]string{"id": "test-identity", "type": dummyType.String()}
						err := json.NewEncoder(w).Encode(identity)
						require.NoError(t, err)
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			expectErr:  false,
			expectedID: "test-identity",
		},
		{
			name: "validation_failed",
			request: v1.ConsumerIdentityForConfigRequest[runtime.Typed]{
				Config: &dummyv1.Repository{},
			},
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
			},
			expectErr: true,
		},
		{
			name: "call_failed",
			request: v1.ConsumerIdentityForConfigRequest[runtime.Typed]{
				Config: &dummyv1.Repository{},
			},
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

			plugin := NewCredentialRepositoryPlugin(server.Client(), "test-plugin", server.URL, types.Config{
				ID:         "test-plugin",
				Type:       types.TCP,
				PluginType: v1.CredentialRepositoryPluginType,
			}, server.URL, dummyCapability([]byte(`{}`)))

			identity, err := plugin.ConsumerIdentityForConfig(context.Background(), tt.request)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedID, identity["id"])
			}
		})
	}
}

func TestResolve(t *testing.T) {
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)

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
				Config: &runtime.Raw{
					Type: dummyType,
					Data: []byte(`{}`),
				},
				Identity: map[string]string{"id": "test-identity"},
			},
			credentials: map[string]string{"key": "value"},
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == Resolve {
						resolved := map[string]string{"resolved": "credentials", "token": "abc123"}
						err := json.NewEncoder(w).Encode(resolved)
						require.NoError(t, err)
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			expectErr:   false,
			expectedKey: "abc123",
		},
		{
			name: "invalid_credentials",
			request: v1.ResolveRequest[runtime.Typed]{
				Config:   &dummyv1.Repository{},
				Identity: map[string]string{"id": "test-identity"},
			},
			credentials: map[string]string{"invalid_key": "invalid_value"},
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				}))
			},
			expectErr: true,
		},
		{
			name: "validation_failed",
			request: v1.ResolveRequest[runtime.Typed]{
				Config:   &dummyv1.Repository{},
				Identity: map[string]string{"id": "test-identity"},
			},
			credentials: map[string]string{"key": "value"},
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
			},
			expectErr: true,
		},
		{
			name: "call_failed",
			request: v1.ResolveRequest[runtime.Typed]{
				Config:   &dummyv1.Repository{},
				Identity: map[string]string{"id": "test-identity"},
			},
			credentials: map[string]string{"key": "value"},
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

			plugin := NewCredentialRepositoryPlugin(server.Client(), "test-plugin", server.URL, types.Config{
				ID:         "test-plugin",
				Type:       types.TCP,
				PluginType: v1.CredentialRepositoryPluginType,
			}, server.URL, dummyCapability([]byte(`{}`)))

			resolved, err := plugin.Resolve(context.Background(), tt.request, tt.credentials)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedKey, resolved["token"])
			}
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
			expectErr: false,
		},
		{
			name: "failed_ping",
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

			plugin := NewCredentialRepositoryPlugin(server.Client(), "test-plugin", server.URL, types.Config{
				ID:         "test-plugin",
				Type:       types.TCP,
				PluginType: v1.CredentialRepositoryPluginType,
			}, server.URL, dummyCapability([]byte(`{}`)))

			err := plugin.Ping(context.Background())
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestToCredentials(t *testing.T) {
	tests := []struct {
		name        string
		credentials map[string]string
		expectErr   bool
	}{
		{name: "valid", credentials: map[string]string{"key": "value"}, expectErr: false},
		{name: "empty", credentials: map[string]string{}, expectErr: false},
		{name: "multiple_keys", credentials: map[string]string{"key1": "value1", "key2": "value2"}, expectErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kv, err := toCredentials(tt.credentials)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, "Authorization", kv.Key)
				require.NotEmpty(t, kv.Value)
			}
		})
	}
}
