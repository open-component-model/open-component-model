package digestprocessor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/runtime"

	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts/digestprocessor/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

func TestProcessResourceDigest(t *testing.T) {
	tests := []struct {
		name        string
		request     *v1.ProcessResourceDigestRequest
		credentials map[string]string
		setupMock   func() *httptest.Server
		expectErr   bool
	}{
		{
			name:        "success",
			request:     &v1.ProcessResourceDigestRequest{},
			credentials: map[string]string{"key": "value"},
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == ProcessResourceDigest {
						err := json.NewEncoder(w).Encode(&v1.ProcessResourceDigestResponse{})
						require.NoError(t, err)
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			expectErr: false,
		},
		{
			name:        "invalid_credentials",
			request:     &v1.ProcessResourceDigestRequest{},
			credentials: map[string]string{"invalid_key": "invalid_value"},
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				}))
			},
			expectErr: true,
		},
		{
			name:        "call_failed",
			request:     &v1.ProcessResourceDigestRequest{},
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

			plugin := NewDigestProcessorPlugin(server.Client(), "test-plugin", server.URL, types.Config{
				ID:         "test-plugin",
				Type:       types.TCP,
				PluginType: v1.DigestProcessorPluginType,
			}, server.URL, dummyCapability([]byte(`{}`)))

			_, err := plugin.ProcessResourceDigest(context.Background(), tt.request, tt.credentials)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
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

			plugin := NewDigestProcessorPlugin(server.Client(), "test-plugin", server.URL, types.Config{
				ID:         "test-plugin",
				Type:       types.TCP,
				PluginType: v1.DigestProcessorPluginType,
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

func TestGetIdentity(t *testing.T) {
	tests := []struct {
		name      string
		request   *v1.GetIdentityRequest[runtime.Typed]
		setupMock func() *httptest.Server
		expectErr bool
	}{
		{
			name:    "success",
			request: &v1.GetIdentityRequest[runtime.Typed]{Typ: &runtime.Raw{Type: dummyType, Data: []byte(`{}`)}},
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == Identity {
						err := json.NewEncoder(w).Encode(&v1.GetIdentityResponse{})
						require.NoError(t, err)
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			expectErr: false,
		},
		{
			name:    "validation_failed",
			request: &v1.GetIdentityRequest[runtime.Typed]{Typ: &runtime.Raw{Type: dummyType, Data: []byte(`{}`)}},
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
			},
			expectErr: true,
		},
		{
			name:    "call_failed",
			request: &v1.GetIdentityRequest[runtime.Typed]{Typ: &runtime.Raw{Type: dummyType, Data: []byte(`{}`)}},
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

			plugin := NewDigestProcessorPlugin(server.Client(), "test-plugin", server.URL, types.Config{
				ID:         "test-plugin",
				Type:       types.TCP,
				PluginType: v1.DigestProcessorPluginType,
			}, server.URL, dummyCapability([]byte(`{}`)))

			_, err := plugin.GetIdentity(context.Background(), tt.request)
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := toCredentials(tt.credentials)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
