package componentversionrepository

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1"
	repov1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestGetComponentVersionHandlerFunc(t *testing.T) {
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)

	tests := []struct {
		name         string
		handlerFunc  func() http.HandlerFunc
		request      func(base string) *http.Request
		assertOutput func(t *testing.T, resp *http.Response)
		assertError  func(t *testing.T, err error)
	}{
		{
			name: "GetComponentVersionHandlerFunc unauthorized error",
			handlerFunc: func() http.HandlerFunc {
				handler := GetComponentVersionHandlerFunc(func(ctx context.Context, request repov1.GetComponentVersionRequest[*v1.OCIRepository], credentials map[string]string) (*descriptor.Descriptor, error) {
					return &descriptor.Descriptor{}, nil
				}, scheme, &v1.OCIRepository{})

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
					Method: "GET",
					URL:    parse,
				}
			},
		},
		{
			name: "GetComponentVersionHandlerFunc success",
			handlerFunc: func() http.HandlerFunc {
				handler := GetComponentVersionHandlerFunc(func(ctx context.Context, request repov1.GetComponentVersionRequest[*v1.OCIRepository], credentials map[string]string) (*descriptor.Descriptor, error) {
					return &descriptor.Descriptor{
						Meta: descriptor.Meta{
							Version: "1.0.0",
						},
						Component: descriptor.Component{
							ComponentMeta: descriptor.ComponentMeta{
								ObjectMeta: descriptor.ObjectMeta{
									Name:    "component",
									Version: "1.0.0",
								},
							},
						},
						Signatures: nil,
					}, nil
				}, scheme, &v1.OCIRepository{})

				return handler
			},
			assertOutput: func(t *testing.T, resp *http.Response) {
				defer resp.Body.Close()
				require.Equal(t, http.StatusOK, resp.StatusCode)
				content, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				require.Equal(t, `{"meta":{"schemaVersion":"1.0.0"},"component":{"name":"component","version":"1.0.0","provider":""}}
`, string(content))

			},
			assertError: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
			request: func(base string) *http.Request {
				header := http.Header{}
				header.Add("Authorization", `{"access_token": "abc"}`)
				parse, _ := url.Parse(base)
				query := parse.Query()
				query.Add("version", "1.0.0")
				query.Add("name", "component")
				parse.RawQuery = query.Encode()

				return &http.Request{
					Method: "GET",
					URL:    parse,
					Header: header,
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

func TestGetLocalResourceHandlerFunc(t *testing.T) {
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)

	tests := []struct {
		name         string
		handlerFunc  func(t *testing.T) http.HandlerFunc
		request      func(base string) *http.Request
		assertOutput func(t *testing.T, resp *http.Response)
		assertError  func(t *testing.T, err error)
	}{
		{
			name: "GetLocalResourceHandlerFunc unauthorized error",
			handlerFunc: func(t *testing.T) http.HandlerFunc {
				handler := GetLocalResourceHandlerFunc(func(ctx context.Context, request repov1.GetLocalResourceRequest[*v1.OCIRepository], credentials map[string]string) error {
					return nil
				}, scheme, &v1.OCIRepository{})

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
					Method: "GET",
					URL:    parse,
				}
			},
		},
		{
			name: "GetLocalResourceHandlerFunc success",
			handlerFunc: func(t *testing.T) http.HandlerFunc {
				handler := GetLocalResourceHandlerFunc(func(ctx context.Context, request repov1.GetLocalResourceRequest[*v1.OCIRepository], credentials map[string]string) error {
					require.Equal(t, "component", request.Name)
					require.Equal(t, "1.0.0", request.Version)
					return nil
				}, scheme, &v1.OCIRepository{})

				return handler
			},
			assertOutput: func(t *testing.T, resp *http.Response) {
				defer resp.Body.Close()
				require.Equal(t, http.StatusOK, resp.StatusCode)
			},
			assertError: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
			request: func(base string) *http.Request {
				header := http.Header{}
				header.Add("Authorization", `{"access_token": "abc"}`)
				parse, _ := url.Parse(base)
				query := parse.Query()
				query.Add("version", "1.0.0")
				query.Add("name", "component")
				parse.RawQuery = query.Encode()

				return &http.Request{
					Method: "GET",
					URL:    parse,
					Header: header,
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := tt.handlerFunc(t)
			testServer := httptest.NewServer(handler)
			defer testServer.Close()
			client := testServer.Client()
			resp, err := client.Do(tt.request(testServer.URL))
			tt.assertError(t, err)
			tt.assertOutput(t, resp)
		})
	}
}

func TestAddComponentVersionHandlerFunc(t *testing.T) {
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)

	tests := []struct {
		name         string
		handlerFunc  func() http.HandlerFunc
		request      func(base string) *http.Request
		assertOutput func(t *testing.T, resp *http.Response)
		assertError  func(t *testing.T, err error)
	}{
		{
			name: "AddComponentVersionHandlerFunc unauthorized error",
			handlerFunc: func() http.HandlerFunc {
				handler := AddComponentVersionHandlerFunc(func(ctx context.Context, request repov1.PostComponentVersionRequest[*v1.OCIRepository], credentials map[string]string) error {
					return nil
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
			name: "AddComponentVersionHandlerFunc success",
			handlerFunc: func() http.HandlerFunc {
				handler := AddComponentVersionHandlerFunc(func(ctx context.Context, request repov1.PostComponentVersionRequest[*v1.OCIRepository], credentials map[string]string) error {
					return nil
				})

				return handler
			},
			assertOutput: func(t *testing.T, resp *http.Response) {
				defer resp.Body.Close()
				require.Equal(t, http.StatusOK, resp.StatusCode)
			},
			assertError: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
			request: func(base string) *http.Request {
				header := http.Header{}
				header.Add("Authorization", `{"access_token": "abc"}`)
				parse, _ := url.Parse(base)
				query := parse.Query()
				query.Add("version", "1.0.0")
				query.Add("name", "component")
				parse.RawQuery = query.Encode()
				body := &bytes.Buffer{}
				body.Write([]byte(`{
  "repository" : {
    "type" : "OCIRepository/1.0.0",
    "baseUrl" : "baseurl"
  },
  "descriptor" : {
    "meta" : {
      "schemaVersion" : "1.0.0"
    },
    "component" : {
      "name" : "component",
      "version" : "1.0.0",
      "provider" : ""
    }
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

func TestAddLocalResourceHandlerFunc(t *testing.T) {
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)

	tests := []struct {
		name         string
		handlerFunc  func() http.HandlerFunc
		request      func(base string) *http.Request
		assertOutput func(t *testing.T, resp *http.Response)
		assertError  func(t *testing.T, err error)
	}{
		{
			name: "AddLocalResourceHandlerFunc unauthorized error",
			handlerFunc: func() http.HandlerFunc {
				handler := AddLocalResourceHandlerFunc(func(ctx context.Context, request repov1.PostLocalResourceRequest[*v1.OCIRepository], credentials map[string]string) (*descriptor.Resource, error) {
					return &descriptor.Resource{}, nil
				}, scheme)

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
			name: "AddLocalResourceHandlerFunc success",
			handlerFunc: func() http.HandlerFunc {
				handler := AddLocalResourceHandlerFunc(func(ctx context.Context, request repov1.PostLocalResourceRequest[*v1.OCIRepository], credentials map[string]string) (*descriptor.Resource, error) {
					res := &descriptor.Resource{
						ElementMeta: descriptor.ElementMeta{
							ObjectMeta: descriptor.ObjectMeta{
								Name:    "name",
								Version: "v1.0.0",
							},
							ExtraIdentity: map[string]string{
								"test": "value",
							},
						},
						Type:     "OCIRepository",
						Relation: "local",
						Access: &runtime.Raw{
							Type: runtime.Type{
								Version: "v1.0.0",
								Name:    "access",
							},
							Data: []byte(`{"access": "method"}`),
						},
					}

					return res, nil
				}, scheme)

				return handler
			},
			assertOutput: func(t *testing.T, resp *http.Response) {
				defer resp.Body.Close()
				require.Equal(t, http.StatusOK, resp.StatusCode)
			},
			assertError: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
			request: func(base string) *http.Request {
				header := http.Header{}
				header.Add("Authorization", `{"access_token": "abc"}`)
				parse, _ := url.Parse(base)
				query := parse.Query()
				query.Add("version", "1.0.0")
				query.Add("name", "component")
				parse.RawQuery = query.Encode()
				body := &bytes.Buffer{}
				body.Write([]byte(`{
  "name" : "name",
  "version" : "v1.0.0",
  "extraIdentity" : {
    "test" : "value"
  },
  "type" : "OCIRepository",
  "relation" : "local",
  "access" : {
    "access" : "method"
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
