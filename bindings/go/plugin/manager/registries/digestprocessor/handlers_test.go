package digestprocessor

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/digestprocessor/v1"

	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestResourceDigestProcessorHandlerFunc(t *testing.T) {
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)

	tests := []struct {
		name         string
		handlerFunc  func() http.HandlerFunc
		request      func(base string) *http.Request
		assertOutput func(t *testing.T, resp *http.Response)
		assertError  func(t *testing.T, err error)
	}{
		{
			name: "ResourceInputProcessorHandlerFunc unauthorized error",
			handlerFunc: func() http.HandlerFunc {
				handler := ResourceDigestProcessorHandlerFunc(func(ctx context.Context, req *v1.ProcessResourceDigestRequest, credentials map[string]string) (*v1.ProcessResourceDigestResponse, error) {
					return &v1.ProcessResourceDigestResponse{}, nil
				})

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
				return &http.Request{
					Method: "POST",
					URL:    parse,
				}
			},
		},
		{
			name: "ResourceInputProcessorHandlerFunc success",
			handlerFunc: func() http.HandlerFunc {
				handler := ResourceDigestProcessorHandlerFunc(func(ctx context.Context, req *v1.ProcessResourceDigestRequest, credentials map[string]string) (*v1.ProcessResourceDigestResponse, error) {
					return &v1.ProcessResourceDigestResponse{
						Resource: &descriptorv2.Resource{
							ElementMeta: descriptorv2.ElementMeta{
								ObjectMeta: descriptorv2.ObjectMeta{
									Name:    "test-resource",
									Version: "v1.0.0",
								},
							},
							Type:     "test-type",
							Relation: "localFile",
						},
					}, nil
				})

				return handler
			},
			assertOutput: func(t *testing.T, resp *http.Response) {
				defer resp.Body.Close()
				require.Equal(t, http.StatusOK, resp.StatusCode)
				content, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				require.Contains(t, string(content), `"name":"test-resource"`)
				require.Contains(t, string(content), `"version":"v1.0.0"`)
				require.Contains(t, string(content), `"type":"test-type"`)
				require.Contains(t, string(content), `"relation":"localFile"`)
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
					"resource": {
						"name": "test-resource",
						"version": "v1.0.0",
						"type": "test-type"
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

func TestIdentityProcessorHandlerFunc(t *testing.T) {
	tests := []struct {
		name         string
		handlerFunc  func() http.HandlerFunc
		request      func(base string) *http.Request
		assertOutput func(t *testing.T, resp *http.Response)
		assertError  func(t *testing.T, err error)
	}{
		{
			name: "IdentityProcessorHandlerFunc missing body error",
			handlerFunc: func() http.HandlerFunc {
				handler := IdentityProcessorHandlerFunc(func(ctx context.Context, req *v1.GetIdentityRequest[runtime.Typed]) (*v1.GetIdentityResponse, error) {
					return &v1.GetIdentityResponse{}, nil
				})
				return handler
			},
			assertOutput: func(t *testing.T, resp *http.Response) {
				require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
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
			name: "IdentityProcessorHandlerFunc success",
			handlerFunc: func() http.HandlerFunc {
				handler := IdentityProcessorHandlerFunc(func(ctx context.Context, req *v1.GetIdentityRequest[runtime.Typed]) (*v1.GetIdentityResponse, error) {
					return &v1.GetIdentityResponse{
						Identity: map[string]string{"id": "test-identity"},
					}, nil
				})
				return handler
			},
			assertOutput: func(t *testing.T, resp *http.Response) {
				defer resp.Body.Close()
				require.Equal(t, http.StatusOK, resp.StatusCode)
				content, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				require.Contains(t, string(content), `"id":"test-identity"`)
			},
			assertError: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
			request: func(base string) *http.Request {
				header := http.Header{}
				parse, _ := url.Parse(base)
				body := &bytes.Buffer{}
				body.Write([]byte(`{"typed":{"type":"example-type"}}`))

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
