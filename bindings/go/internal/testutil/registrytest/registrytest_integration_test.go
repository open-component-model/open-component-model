package registrytest_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/internal/testutil/registrytest"
)

func Test_Integration_Start(t *testing.T) {
	tests := []struct {
		name          string
		opts          []registrytest.Option
		anonymousCode int
	}{
		{name: "anonymous", anonymousCode: http.StatusOK},
		{
			name:          "with auth",
			opts:          []registrytest.Option{registrytest.WithAuth("ocm", "secret")},
			anonymousCode: http.StatusUnauthorized,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			reg := registrytest.Start(t, tt.opts...)
			r.NotEmpty(reg.Address)

			r.Equal(tt.anonymousCode, ping(t, http.DefaultClient, reg.Address))
			if reg.Username == "" {
				return
			}
			client := registrytest.AuthClient(reg.Address, reg.Username, reg.Password)
			r.Equal(http.StatusOK, ping(t, client, reg.Address))
		})
	}
}

// httpDoer is the part of *http.Client and *auth.Client that ping needs.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// ping sends GET /v2/ to the registry at address and returns the status code.
func ping(t *testing.T, client httpDoer, address string) int {
	t.Helper()
	r := require.New(t)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+address+"/v2/", nil)
	r.NoError(err)
	resp, err := client.Do(req)
	r.NoError(err)
	_ = resp.Body.Close()
	return resp.StatusCode
}
