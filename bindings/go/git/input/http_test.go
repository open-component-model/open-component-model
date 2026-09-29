package input_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/git/input"
	inputspec "ocm.software/open-component-model/bindings/go/git/spec/input"
	inputv1 "ocm.software/open-component-model/bindings/go/git/spec/input/v1"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
)

// The test server's certificate is self-signed, so the request only reaches it
// when the configured InsecureSkipVerify is applied.
func TestProcessResourceUsesHTTPConfig(t *testing.T) {
	for _, tc := range []struct {
		name        string
		insecure    bool
		wantReached bool
		wantErr     string
	}{
		{name: "configured client trusts the server", insecure: true, wantReached: true, wantErr: "503"},
		{name: "verifying client rejects the server", insecure: false, wantReached: false, wantErr: "certificate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			var reached atomic.Bool
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				reached.Store(true)
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			t.Cleanup(server.Close)
			maxRetries := -1
			method := &input.InputMethod{
				TempFolder: t.TempDir(),
				HTTPConfig: &httpv1alpha1.Config{
					TLSConfig: httpv1alpha1.TLSConfig{InsecureSkipVerify: &tc.insecure},
					Retry:     &httpv1alpha1.RetryConfig{MaxRetries: &maxRetries},
				},
			}

			result, err := method.ProcessResource(t.Context(), &constructorruntime.Resource{Input: &inputv1.Git{
				Type: inputspec.V1VersionedType, Repository: server.URL + "/repo.git",
			}}, nil)
			r.ErrorContains(err, tc.wantErr)
			r.Nil(result)
			r.Equal(tc.wantReached, reached.Load())
		})
	}
}
