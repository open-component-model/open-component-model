package signinghandler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"

	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/signing/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

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

			plugin := NewSigningHandlerPlugin(server.Client(), "test-plugin", server.URL, types.Config{
				ID:         "test-plugin",
				Type:       types.TCP,
				PluginType: v1.SigningHandlerPluginType,
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

func TestGetSignerIdentity(t *testing.T) {
	tests := []struct {
		name      string
		request   *v1.GetSignerIdentityRequest[runtime.Typed]
		setupMock func() *httptest.Server
		expectErr bool
	}{
		{
			name:    "success",
			request: &v1.GetSignerIdentityRequest[runtime.Typed]{Name: "sig", SignRequest: v1.SignRequest[runtime.Typed]{Config: &runtime.Raw{Type: dummyType, Data: []byte(`{"key":"val"}`)}}},
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == GetSignerIdentity {
						_ = json.NewEncoder(w).Encode(&v1.IdentityResponse{Identity: map[string]string{"id": "signer"}})
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			expectErr: false,
		},
		{
			name:    "validation_failed",
			request: &v1.GetSignerIdentityRequest[runtime.Typed]{Name: "sig", SignRequest: v1.SignRequest[runtime.Typed]{Config: &runtime.Raw{Type: dummyType, Data: []byte(`{"key":"val"}`)}}},
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
			},
			expectErr: true,
		},
		{
			name:    "call_failed",
			request: &v1.GetSignerIdentityRequest[runtime.Typed]{Name: "sig", SignRequest: v1.SignRequest[runtime.Typed]{Config: &runtime.Raw{Type: dummyType, Data: []byte(`{"k":1}`)}}},
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

			plugin := NewSigningHandlerPlugin(server.Client(), "test-plugin", server.URL, types.Config{
				ID:         "test-plugin",
				Type:       types.TCP,
				PluginType: v1.SigningHandlerPluginType,
			}, server.URL, dummyCapability([]byte(`{}`)))

			_, err := plugin.GetSignerIdentity(context.Background(), tt.request)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGetVerifierIdentity(t *testing.T) {
	tests := []struct {
		name      string
		request   *v1.GetVerifierIdentityRequest[runtime.Typed]
		setupMock func() *httptest.Server
		expectErr bool
	}{
		{
			name: "success",
			request: &v1.GetVerifierIdentityRequest[runtime.Typed]{VerifyRequest: v1.VerifyRequest[runtime.Typed]{
				Config: &runtime.Raw{Type: dummyType, Data: []byte(`{"x":true}`)},
			}},
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == GetVerifierIdentity {
						_ = json.NewEncoder(w).Encode(&v1.IdentityResponse{Identity: map[string]string{"id": "verifier"}})
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			expectErr: false,
		},
		{
			name: "validation_failed",
			request: &v1.GetVerifierIdentityRequest[runtime.Typed]{VerifyRequest: v1.VerifyRequest[runtime.Typed]{
				Config: &runtime.Raw{Type: dummyType, Data: []byte(`{"x":true}`)},
			}},
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
			},
			expectErr: true,
		},
		{
			name: "call_failed",
			request: &v1.GetVerifierIdentityRequest[runtime.Typed]{VerifyRequest: v1.VerifyRequest[runtime.Typed]{
				Config: &runtime.Raw{Type: dummyType, Data: []byte(`{"x":true}`)}}},
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

			plugin := NewSigningHandlerPlugin(server.Client(), "test-plugin", server.URL, types.Config{
				ID:         "test-plugin",
				Type:       types.TCP,
				PluginType: v1.SigningHandlerPluginType,
			}, server.URL, dummyCapability([]byte(`{}`)))

			_, err := plugin.GetVerifierIdentity(context.Background(), tt.request)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSign(t *testing.T) {
	tests := []struct {
		name        string
		request     *v1.SignRequest[runtime.Typed]
		credentials map[string]string
		setupMock   func() *httptest.Server
		expectErr   bool
	}{
		{
			name: "success",
			request: &v1.SignRequest[runtime.Typed]{
				Digest: &v2.Digest{HashAlgorithm: "sha256", NormalisationAlgorithm: "ociArtifactDigest/v1", Value: "abc"},
				Config: &runtime.Raw{Type: dummyType, Data: []byte(`{"k":"v"}`)},
			},
			credentials: map[string]string{"key": "value"},
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == Sign {
						_ = json.NewEncoder(w).Encode(&v1.SignResponse{Signature: &v2.SignatureInfo{Algorithm: "rsa", Value: "sig", MediaType: "text/plain"}})
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			expectErr: false,
		},
		{
			name:        "invalid_credentials",
			request:     &v1.SignRequest[runtime.Typed]{Digest: &v2.Digest{}, Config: &runtime.Raw{Type: dummyType, Data: []byte(`{}`)}},
			credentials: map[string]string{"invalid": "creds"},
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				}))
			},
			expectErr: true,
		},
		{
			name:        "call_failed",
			request:     &v1.SignRequest[runtime.Typed]{Digest: &v2.Digest{}, Config: &runtime.Raw{Type: dummyType, Data: []byte(`{}`)}},
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

			plugin := NewSigningHandlerPlugin(server.Client(), "test-plugin", server.URL, types.Config{
				ID:         "test-plugin",
				Type:       types.TCP,
				PluginType: v1.SigningHandlerPluginType,
			}, server.URL, dummyCapability([]byte(`{}`)))

			_, err := plugin.Sign(context.Background(), tt.request, tt.credentials)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVerify(t *testing.T) {
	tests := []struct {
		name        string
		request     *v1.VerifyRequest[runtime.Typed]
		credentials map[string]string
		setupMock   func() *httptest.Server
		expectErr   bool
	}{
		{
			name: "call_failed",
			request: &v1.VerifyRequest[runtime.Typed]{
				Signature: &v2.Signature{Signature: v2.SignatureInfo{Algorithm: "rsa", Value: "sig"}},
				Config:    &runtime.Raw{Type: dummyType, Data: []byte(`{}`)},
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

			plugin := NewSigningHandlerPlugin(server.Client(), "test-plugin", server.URL, types.Config{
				ID:         "test-plugin",
				Type:       types.TCP,
				PluginType: v1.SigningHandlerPluginType,
			}, server.URL, dummyCapability([]byte(`{}`)))

			_, err := plugin.Verify(context.Background(), tt.request, tt.credentials)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
