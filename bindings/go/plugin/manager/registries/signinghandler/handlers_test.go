package signinghandler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/signing/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type stubSigningPlugin[T runtime.Typed] struct {
	contracts.EmptyBasePlugin
}

func (s *stubSigningPlugin[T]) GetSignerIdentity(ctx context.Context, req *v1.GetSignerIdentityRequest[T]) (*v1.IdentityResponse, error) {
	return &v1.IdentityResponse{Identity: map[string]string{"id": "signer"}}, nil
}

func (s *stubSigningPlugin[T]) GetVerifierIdentity(ctx context.Context, req *v1.GetVerifierIdentityRequest[T]) (*v1.IdentityResponse, error) {
	return &v1.IdentityResponse{Identity: map[string]string{"id": "verifier"}}, nil
}

func (s *stubSigningPlugin[T]) Sign(ctx context.Context, request *v1.SignRequest[T], credentials map[string]string) (*v1.SignResponse, error) {
	return &v1.SignResponse{Signature: &v2.SignatureInfo{Algorithm: "rsa", Value: credentials["test"]}}, nil
}

func (s *stubSigningPlugin[T]) Verify(ctx context.Context, request *v1.VerifyRequest[T], credentials map[string]string) (*v1.VerifyResponse, error) {
	return &v1.VerifyResponse{}, nil
}

var _ v1.SignatureHandlerContract[runtime.Typed] = &stubSigningPlugin[runtime.Typed]{}

func TestHandleGetSignerIdentity(t *testing.T) {
	tests := []struct {
		name         string
		handlerFunc  func() http.HandlerFunc
		request      func(ctx context.Context, base string) (*http.Request, error)
		assertOutput func(t *testing.T, resp *http.Response)
		assertError  func(t *testing.T, err error)
	}{
		{
			name: "missing body returns 400",
			handlerFunc: func() http.HandlerFunc {
				return handleGetSignerIdentity(&stubSigningPlugin[*runtime.Raw]{})
			},
			request: func(ctx context.Context, base string) (*http.Request, error) {
				parse, _ := url.Parse(base)
				return http.NewRequestWithContext(ctx, http.MethodPost, parse.String(), nil)
			},
			assertOutput: func(t *testing.T, resp *http.Response) {
				require.Equal(t, http.StatusBadRequest, resp.StatusCode)
			},
			assertError: func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name: "success",
			handlerFunc: func() http.HandlerFunc {
				return handleGetSignerIdentity(&stubSigningPlugin[*runtime.Raw]{})
			},
			request: func(ctx context.Context, base string) (*http.Request, error) {
				parse, _ := url.Parse(base)
				req := v1.GetSignerIdentityRequest[runtime.Typed]{
					SignRequest: v1.SignRequest[runtime.Typed]{
						Config: &runtime.Raw{
							Type: runtime.NewVersionedType("dummy", "v1"),
							Data: []byte(`{"type": "dummy/v1"}`),
						},
					},
					Name: "sig",
				}
				body, err := json.Marshal(req)
				if err != nil {
					return nil, err
				}
				return http.NewRequestWithContext(ctx, http.MethodPost, parse.String(), bytes.NewReader(body))
			},
			assertOutput: func(t *testing.T, resp *http.Response) {
				defer resp.Body.Close()
				require.Equal(t, http.StatusOK, resp.StatusCode)
			},
			assertError: func(t *testing.T, err error) { require.NoError(t, err) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := tt.handlerFunc()
			testServer := httptest.NewServer(handler)
			defer testServer.Close()
			client := testServer.Client()
			req, err := tt.request(t.Context(), testServer.URL)
			require.NoError(t, err)
			resp, err := client.Do(req)
			tt.assertError(t, err)
			tt.assertOutput(t, resp)
		})
	}
}

func TestHandleVerifyAndSign(t *testing.T) {
	// Verify
	t.Run("verify", func(t *testing.T) {
		handler := handleVerify[*runtime.Raw](&stubSigningPlugin[*runtime.Raw]{})
		srv := httptest.NewServer(handler)
		defer srv.Close()
		reqBody := bytes.NewBufferString(`{"signature":{"name":"n","digest":{"hashAlgorithm":"sha256","normalisationAlgorithm":"ociArtifactDigest/v1","value":"abc"}},"config":{"type":"dummy/v1"}}`)
		resp, err := srv.Client().Post(srv.URL, "application/json", reqBody)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		_ = resp.Body.Close()
	})

	// Sign
	t.Run("sign", func(t *testing.T) {
		handler := handleSign[*runtime.Raw](&stubSigningPlugin[*runtime.Raw]{})
		srv := httptest.NewServer(handler)
		defer srv.Close()
		reqBody := bytes.NewBufferString(`{"digest":{"hashAlgorithm":"sha256","normalisationAlgorithm":"ociArtifactDigest/v1","value":"abc"},"config":{"type":"dummy/v1"}}`)
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL, reqBody)
		req.Header.Set("Authorization", `{"test":"value"}`)
		require.NoError(t, err)
		resp, err := srv.Client().Do(req)
		t.Cleanup(func() {
			require.NoError(t, resp.Body.Close())
		})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var response v1.SignResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&response))
		require.Equal(t, "value", response.Signature.Value)
	})
}
