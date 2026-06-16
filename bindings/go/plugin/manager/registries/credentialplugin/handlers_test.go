package credentialplugin

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
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentialplugin/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestGetConsumerIdentityHandlerFunc(t *testing.T) {
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	dummyRepo := &dummyv1.Repository{}

	tests := []struct {
		name         string
		handlerFunc  func() http.HandlerFunc
		request      func(base string) *http.Request
		assertOutput func(t *testing.T, resp *http.Response)
	}{
		{
			name: "missing body returns 400",
			handlerFunc: func() http.HandlerFunc {
				return GetConsumerIdentityHandlerFunc(func(ctx context.Context, req v1.GetConsumerIdentityRequest[*dummyv1.Repository]) (runtime.Identity, error) {
					return map[string]string{"id": "test-identity"}, nil
				}, scheme, dummyRepo)
			},
			assertOutput: func(t *testing.T, resp *http.Response) {
				require.Equal(t, http.StatusBadRequest, resp.StatusCode)
			},
			request: func(base string) *http.Request {
				parse, _ := url.Parse(base)
				return &http.Request{Method: http.MethodPost, URL: parse, Body: nil}
			},
		},
		{
			name: "success returns identity JSON",
			handlerFunc: func() http.HandlerFunc {
				return GetConsumerIdentityHandlerFunc(func(ctx context.Context, req v1.GetConsumerIdentityRequest[*dummyv1.Repository]) (runtime.Identity, error) {
					return map[string]string{"id": "test-identity", "type": "test-type"}, nil
				}, scheme, dummyRepo)
			},
			assertOutput: func(t *testing.T, resp *http.Response) {
				defer resp.Body.Close()
				require.Equal(t, http.StatusOK, resp.StatusCode)
				content, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				require.Contains(t, string(content), `"id":"test-identity"`)
				require.Contains(t, string(content), `"type":"test-type"`)
			},
			request: func(base string) *http.Request {
				parse, _ := url.Parse(base)
				body := &bytes.Buffer{}
				body.WriteString(`{"credential": {"type": "DummyRepository", "baseUrl": "test-url"}}`)
				return &http.Request{Method: http.MethodPost, URL: parse, Body: io.NopCloser(body)}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testServer := httptest.NewServer(tt.handlerFunc())
			defer testServer.Close()
			resp, err := testServer.Client().Do(tt.request(testServer.URL))
			require.NoError(t, err)
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
	}{
		{
			name: "malformed Authorization header returns 401",
			handlerFunc: func() http.HandlerFunc {
				return ResolveHandlerFunc(func(ctx context.Context, req v1.ResolveRequest[*dummyv1.Repository], credentials map[string]string) (map[string]string, error) {
					return map[string]string{"resolved": "credentials"}, nil
				}, scheme, dummyRepo)
			},
			assertOutput: func(t *testing.T, resp *http.Response) {
				require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
			},
			request: func(base string) *http.Request {
				parse, _ := url.Parse(base)
				body := &bytes.Buffer{}
				body.WriteString(`{"identity": {"id": "test-identity"}}`)
				return &http.Request{Method: http.MethodPost, URL: parse, Body: io.NopCloser(body)}
			},
		},
		{
			name: "missing body returns 400",
			handlerFunc: func() http.HandlerFunc {
				return ResolveHandlerFunc(func(ctx context.Context, req v1.ResolveRequest[*dummyv1.Repository], credentials map[string]string) (map[string]string, error) {
					return map[string]string{"resolved": "credentials"}, nil
				}, scheme, dummyRepo)
			},
			assertOutput: func(t *testing.T, resp *http.Response) {
				require.Equal(t, http.StatusBadRequest, resp.StatusCode)
			},
			request: func(base string) *http.Request {
				header := http.Header{}
				header.Add("Authorization", `{"access_token": "abc"}`)
				parse, _ := url.Parse(base)
				return &http.Request{Method: http.MethodPost, URL: parse, Header: header, Body: nil}
			},
		},
		{
			name: "success returns resolved credentials JSON",
			handlerFunc: func() http.HandlerFunc {
				return ResolveHandlerFunc(func(ctx context.Context, req v1.ResolveRequest[*dummyv1.Repository], credentials map[string]string) (map[string]string, error) {
					require.Equal(t, "abc", credentials["access_token"])
					require.Equal(t, "test-identity", req.Identity["id"])
					return map[string]string{"resolved": "credentials", "token": "abc123"}, nil
				}, scheme, dummyRepo)
			},
			assertOutput: func(t *testing.T, resp *http.Response) {
				defer resp.Body.Close()
				require.Equal(t, http.StatusOK, resp.StatusCode)
				content, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				require.Contains(t, string(content), `"resolved":"credentials"`)
				require.Contains(t, string(content), `"token":"abc123"`)
			},
			request: func(base string) *http.Request {
				header := http.Header{}
				header.Add("Authorization", `{"access_token": "abc"}`)
				parse, _ := url.Parse(base)
				body := &bytes.Buffer{}
				body.WriteString(`{"identity": {"id": "test-identity"}}`)
				return &http.Request{Method: http.MethodPost, URL: parse, Header: header, Body: io.NopCloser(body)}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testServer := httptest.NewServer(tt.handlerFunc())
			defer testServer.Close()
			resp, err := testServer.Client().Do(tt.request(testServer.URL))
			require.NoError(t, err)
			tt.assertOutput(t, resp)
		})
	}
}
