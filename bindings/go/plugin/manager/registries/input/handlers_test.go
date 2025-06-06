package input

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	inputv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/input/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestResourceInputProcessorHandlerFunc(t *testing.T) {
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
				handler := ResourceInputProcessorHandlerFunc(func(ctx context.Context, request *inputv1.ProcessResourceInputRequest, credentials map[string]string) (*inputv1.ProcessResourceResponse, error) {
					return &inputv1.ProcessResourceResponse{}, nil
				}, scheme, &dummyv1.Repository{})

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
				handler := ResourceInputProcessorHandlerFunc(func(ctx context.Context, request *inputv1.ProcessResourceInputRequest, credentials map[string]string) (*inputv1.ProcessResourceResponse, error) {
					return &inputv1.ProcessResourceResponse{
						Resource: &constructorv1.Resource{
							ElementMeta: constructorv1.ElementMeta{
								ObjectMeta: constructorv1.ObjectMeta{
									Name:    "test-resource",
									Version: "v1.0.0",
								},
							},
							Type:     "test-type",
							Relation: "local",
						},
						Location: &types.Location{
							LocationType: types.LocationTypeLocalFile,
							Value:        "/tmp/test-file",
						},
					}, nil
				}, scheme, &dummyv1.Repository{})

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
				require.Contains(t, string(content), `"type":"localFile"`)
				require.Contains(t, string(content), `"value":"/tmp/test-file"`)
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

func TestSourceInputProcessorHandlerFunc(t *testing.T) {
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
			name: "SourceInputProcessorHandlerFunc unauthorized error",
			handlerFunc: func() http.HandlerFunc {
				handler := SourceInputProcessorHandlerFunc(func(ctx context.Context, request *inputv1.ProcessSourceInputRequest, credentials map[string]string) (*inputv1.ProcessSourceResponse, error) {
					return &inputv1.ProcessSourceResponse{}, nil
				}, scheme, &dummyv1.Repository{})

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
			name: "SourceInputProcessorHandlerFunc success",
			handlerFunc: func() http.HandlerFunc {
				handler := SourceInputProcessorHandlerFunc(func(ctx context.Context, request *inputv1.ProcessSourceInputRequest, credentials map[string]string) (*inputv1.ProcessSourceResponse, error) {
					return &inputv1.ProcessSourceResponse{
						Source: &constructorv1.Source{
							ElementMeta: constructorv1.ElementMeta{
								ObjectMeta: constructorv1.ObjectMeta{
									Name:    "test-source",
									Version: "v1.0.0",
								},
							},
							Type: "test-type",
						},
						Location: &types.Location{
							LocationType: types.LocationTypeLocalFile,
							Value:        "/tmp/test-file",
						},
					}, nil
				}, scheme, &dummyv1.Repository{})

				return handler
			},
			assertOutput: func(t *testing.T, resp *http.Response) {
				defer resp.Body.Close()
				require.Equal(t, http.StatusOK, resp.StatusCode)
				content, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				require.Contains(t, string(content), `"name":"test-source"`)
				require.Contains(t, string(content), `"version":"v1.0.0"`)
				require.Contains(t, string(content), `"type":"test-type"`)
				require.Contains(t, string(content), `"type":"localFile"`)
				require.Contains(t, string(content), `"value":"/tmp/test-file"`)
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
					"source": {
						"name": "test-source",
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
