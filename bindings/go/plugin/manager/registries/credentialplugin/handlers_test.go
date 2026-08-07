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

	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentialplugin/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestGetConsumerIdentityHandlerFunc(t *testing.T) {
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
				})
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
				})
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
	var (
		nilCredsObserved bool
		successCreds     runtime.Typed
		successIdentity  string
	)

	tests := []struct {
		name         string
		handlerFunc  func() http.HandlerFunc
		request      func(base string) *http.Request
		assertOutput func(t *testing.T, resp *http.Response)
	}{
		{
			name: "malformed Authorization header returns 401",
			handlerFunc: func() http.HandlerFunc {
				return ResolveHandlerFunc(func(ctx context.Context, req v1.ResolveRequest[*dummyv1.Repository], credentials runtime.Typed) (runtime.Typed, error) {
					return &runtime.Raw{Data: []byte(`{"resolved":"credentials"}`)}, nil
				})
			},
			assertOutput: func(t *testing.T, resp *http.Response) {
				require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
			},
			request: func(base string) *http.Request {
				header := http.Header{}
				header.Add("Authorization", "not-json")
				parse, _ := url.Parse(base)
				body := &bytes.Buffer{}
				body.WriteString(`{"identity": {"id": "test-identity"}}`)
				return &http.Request{Method: http.MethodPost, URL: parse, Header: header, Body: io.NopCloser(body)}
			},
		},
		{
			name: "missing Authorization header is accepted with nil credentials",
			handlerFunc: func() http.HandlerFunc {
				return ResolveHandlerFunc(func(ctx context.Context, req v1.ResolveRequest[*dummyv1.Repository], credentials runtime.Typed) (runtime.Typed, error) {
					nilCredsObserved = credentials == nil
					return &runtime.Raw{Data: []byte(`{"resolved":"credentials"}`)}, nil
				})
			},
			assertOutput: func(t *testing.T, resp *http.Response) {
				require.Equal(t, http.StatusOK, resp.StatusCode)
				require.True(t, nilCredsObserved, "missing Authorization header must yield nil credentials")
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
				return ResolveHandlerFunc(func(ctx context.Context, req v1.ResolveRequest[*dummyv1.Repository], credentials runtime.Typed) (runtime.Typed, error) {
					return &runtime.Raw{Data: []byte(`{"resolved":"credentials"}`)}, nil
				})
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
				return ResolveHandlerFunc(func(ctx context.Context, req v1.ResolveRequest[*dummyv1.Repository], credentials runtime.Typed) (runtime.Typed, error) {
					successCreds = credentials
					successIdentity = req.Identity["id"]
					return &runtime.Raw{Data: []byte(`{"resolved":"credentials","token":"abc123"}`)}, nil
				})
			},
			assertOutput: func(t *testing.T, resp *http.Response) {
				defer resp.Body.Close()
				require.Equal(t, http.StatusOK, resp.StatusCode)
				require.NotNil(t, successCreds)
				require.Equal(t, "test-identity", successIdentity)
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
