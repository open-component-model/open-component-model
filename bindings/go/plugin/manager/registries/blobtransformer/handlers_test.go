package blobtransformer

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
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/blobtransformer/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestTransformBlobHandlerFunc(t *testing.T) {
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
			name: "TransformBlobHandlerFunc unauthorized error",
			handlerFunc: func() http.HandlerFunc {
				handler := TransformBlobHandlerFunc(func(ctx context.Context, request *v1.TransformBlobRequest[*dummyv1.Repository], credentials map[string]string) (*v1.TransformBlobResponse, error) {
					return &v1.TransformBlobResponse{}, nil
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
			name: "TransformBlobHandlerFunc success",
			handlerFunc: func() http.HandlerFunc {
				handler := TransformBlobHandlerFunc(func(ctx context.Context, request *v1.TransformBlobRequest[*dummyv1.Repository], credentials map[string]string) (*v1.TransformBlobResponse, error) {
					require.Equal(t, "DummyRepository/v1", request.Specification.Type.String())
					return &v1.TransformBlobResponse{
						Location: types.Location{
							LocationType: types.LocationTypeLocalFile,
							Value:        "/dummy/local-file",
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
				require.Contains(t, string(content), "/dummy/local-file")
			},
			assertError: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
			request: func(base string) *http.Request {
				header := http.Header{}
				header.Add("Authorization", `{"access_token": "abc"}`)
				parse, _ := url.Parse(base)
				body := &bytes.Buffer{}
				body.Write([]byte(`{"specification":{"type":"DummyRepository/v1","baseUrl":"ocm.software"}}`))
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
