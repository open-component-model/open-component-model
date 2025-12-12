package componentlister

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/componentlister/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestListComponentsHandlerFunc(t *testing.T) {
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
			name: "ListComponentsHandlerFunc unauthorized error",
			handlerFunc: func() http.HandlerFunc {
				handler := ListComponentsHandlerFunc(func(ctx context.Context, request *v1.ListComponentsRequest[*dummyv1.Repository], credentials map[string]string) (*v1.ListComponentsResponse, error) {
					return &v1.ListComponentsResponse{}, nil
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
					Method: http.MethodPost,
					URL:    parse,
				}
			},
		},
		{
			name: "ListComponentsHandlerFunc success",
			handlerFunc: func() http.HandlerFunc {
				handler := ListComponentsHandlerFunc(func(ctx context.Context, request *v1.ListComponentsRequest[*dummyv1.Repository], credentials map[string]string) (*v1.ListComponentsResponse, error) {
					return &v1.ListComponentsResponse{List: []string{"test-component-1", "test-component-2"}}, nil
				}, scheme, &dummyv1.Repository{})

				return handler
			},
			assertOutput: func(t *testing.T, resp *http.Response) {
				defer resp.Body.Close()
				require.Equal(t, http.StatusOK, resp.StatusCode)
				bites, err := io.ReadAll(resp.Body)
				content := strings.TrimSpace(string(bites))
				require.NoError(t, err)
				require.Equal(t, `{"list":["test-component-1","test-component-2"]}`, content)
			},
			assertError: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
			request: func(base string) *http.Request {
				header := http.Header{}
				header.Add("Authorization", `{"access_token": "abc"}`)
				parse, _ := url.Parse(base)

				return &http.Request{
					Method: http.MethodPost,
					URL:    parse,
					Header: header,
					Body:   io.NopCloser(strings.NewReader(`{"repository":{"type":"DummyRepository/v1","baseUrl":""},"last":""}`)),
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
