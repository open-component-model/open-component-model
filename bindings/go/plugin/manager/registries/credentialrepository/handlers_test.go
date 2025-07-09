package credentialrepository

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestConsumerIdentityForConfigHandlerFunc(t *testing.T) {
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	dummyRepo := &dummyv1.Repository{}

	tests := []struct {
		name         string
		handlerFunc  func() http.HandlerFunc
		request      func(base string) *http.Request
		assertOutput func(t *testing.T, resp *http.Response)
		assertError  func(t *testing.T, err error)
	}{
		{
			name: "ConsumerIdentityForConfigHandlerFunc missing body error",
			handlerFunc: func() http.HandlerFunc {
				handler := ConsumerIdentityForConfigHandlerFunc(func(ctx context.Context, cfg v1.ConsumerIdentityForConfigRequest[*dummyv1.Repository]) (runtime.Identity, error) {
					return map[string]string{"id": "test-identity"}, nil
				}, scheme, dummyRepo)

				return handler
			},
			assertOutput: func(t *testing.T, resp *http.Response) {
				require.Equal(t, http.StatusBadRequest, resp.StatusCode)
			},
			assertError: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
			request: func(base string) *http.Request {
				parse, _ := url.Parse(base)
				return &http.Request{
					Method: "POST",
					URL:    parse,
					Body:   nil,
				}
			},
		},
		{
			name: "ConsumerIdentityForConfigHandlerFunc success",
			handlerFunc: func() http.HandlerFunc {
				handler := ConsumerIdentityForConfigHandlerFunc(func(ctx context.Context, cfg v1.ConsumerIdentityForConfigRequest[*dummyv1.Repository]) (runtime.Identity, error) {
					return map[string]string{"id": "test-identity", "type": "test-type"}, nil
				}, scheme, dummyRepo)

				return handler
			},
			assertOutput: func(t *testing.T, resp *http.Response) {
				defer resp.Body.Close()
				require.Equal(t, http.StatusOK, resp.StatusCode)
				content, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				require.Contains(t, string(content), `"id":"test-identity"`)
				require.Contains(t, string(content), `"type":"test-type"`)
			},
			assertError: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
			request: func(base string) *http.Request {
				parse, _ := url.Parse(base)
				body := &bytes.Buffer{}
				body.Write([]byte(`{
					"config": {
						"type": "DummyRepository",
						"baseUrl": "test-url"
					}
				}`))

				return &http.Request{
					Method: "POST",
					URL:    parse,
					Body:   io.NopCloser(body),
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := tt.handlerFunc()
			testServer := httptest.NewServer(handler)
			defer testServer.Close()
			client := testServer.Client()
			resp, err := client.Do(tt.request(testServer.URL))
			tt.assertError(t, err)
			tt.assertOutput(t, resp)
		})
	}
}

func TestResolveHandlerFunc(t *testing.T) {
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	dummyRepo := &dummyv1.Repository{}

	tests := []struct {
		name         string
		handlerFunc  func() http.HandlerFunc
		request      func(base string) *http.Request
		assertOutput func(t *testing.T, resp *http.Response)
		assertError  func(t *testing.T, err error)
	}{
		{
			name: "ResolveHandlerFunc unauthorized error",
			handlerFunc: func() http.HandlerFunc {
				handler := ResolveHandlerFunc(func(ctx context.Context, cfg v1.ResolveRequest[*dummyv1.Repository], credentials map[string]string) (map[string]string, error) {
					return map[string]string{"resolved": "credentials"}, nil
				}, scheme, dummyRepo)

				return handler
			},
			assertOutput: func(t *testing.T, resp *http.Response) {
				require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
			},
			assertError: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
			request: func(base string) *http.Request {
				parse, _ := url.Parse(base)
				body := &bytes.Buffer{}
				body.Write([]byte(`{
					"config": {
						"type": "DummyRepository",
						"baseUrl": "test-url"
					},
					"identity": {
						"id": "test-identity"
					}
				}`))

				return &http.Request{
					Method: "POST",
					URL:    parse,
					Body:   io.NopCloser(body),
				}
			},
		},
		{
			name: "ResolveHandlerFunc missing body error",
			handlerFunc: func() http.HandlerFunc {
				handler := ResolveHandlerFunc(func(ctx context.Context, cfg v1.ResolveRequest[*dummyv1.Repository], credentials map[string]string) (map[string]string, error) {
					return map[string]string{"resolved": "credentials"}, nil
				}, scheme, dummyRepo)

				return handler
			},
			assertOutput: func(t *testing.T, resp *http.Response) {
				require.Equal(t, http.StatusBadRequest, resp.StatusCode)
			},
			assertError: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
			request: func(base string) *http.Request {
				header := http.Header{}
				header.Add("Authorization", `{"access_token": "abc"}`)
				parse, _ := url.Parse(base)

				return &http.Request{
					Method: "POST",
					URL:    parse,
					Header: header,
					Body:   nil,
				}
			},
		},
		{
			name: "ResolveHandlerFunc success",
			handlerFunc: func() http.HandlerFunc {
				handler := ResolveHandlerFunc(func(ctx context.Context, cfg v1.ResolveRequest[*dummyv1.Repository], credentials map[string]string) (map[string]string, error) {
					return map[string]string{"resolved": "credentials", "token": "abc123"}, nil
				}, scheme, dummyRepo)

				return handler
			},
			assertOutput: func(t *testing.T, resp *http.Response) {
				defer resp.Body.Close()
				require.Equal(t, http.StatusOK, resp.StatusCode)
				content, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				require.Contains(t, string(content), `"resolved":"credentials"`)
				require.Contains(t, string(content), `"token":"abc123"`)
			},
			assertError: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
			request: func(base string) *http.Request {
				header := http.Header{}
				header.Add("Authorization", `{"access_token": "abc"}`)
				parse, _ := url.Parse(base)
				body := &bytes.Buffer{}
				body.Write([]byte(`{
					"config": {
						"type": "DummyRepository",
						"baseUrl": "test-url"
					},
					"identity": {
						"id": "test-identity"
					}
				}`))

				return &http.Request{
					Method: "POST",
					URL:    parse,
					Header: header,
					Body:   io.NopCloser(body),
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := tt.handlerFunc()
			testServer := httptest.NewServer(handler)
			defer testServer.Close()
			client := testServer.Client()
			resp, err := client.Do(tt.request(testServer.URL))
			tt.assertError(t, err)
			tt.assertOutput(t, resp)
		})
	}
}
