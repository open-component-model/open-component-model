package httpauth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/wget/httpauth"
	credv1 "ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1"
)

func TestApply_Redirect(t *testing.T) {
	creds := &credv1.WgetCredentials{
		Type:     credv1.WgetCredentialsVersionedType,
		Username: "user",
		Password: "secret",
	}

	// final records the Authorization header of the redirected request; both servers listen
	// on 127.0.0.1, so net/http itself treats the redirect as same-host.
	var gotAuth string
	final := func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}
	plain := httptest.NewServer(http.HandlerFunc(final))
	t.Cleanup(plain.Close)
	secure := httptest.NewTLSServer(http.HandlerFunc(final))
	t.Cleanup(secure.Close)

	tests := []struct {
		name     string
		target   string
		wantAuth bool
	}{
		{"https to http drops credentials", plain.URL, false},
		{"https to https keeps credentials", secure.URL, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			gotAuth = ""
			origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				http.Redirect(w, req, tc.target+"/final", http.StatusFound)
			}))
			t.Cleanup(origin.Close)

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, origin.URL, nil)
			r.NoError(err)
			client := origin.Client()
			// origin and secure use different self-signed certificates; trust both.
			client.Transport.(*http.Transport).TLSClientConfig.RootCAs.AddCert(secure.Certificate())
			r.NoError(httpauth.Apply(t.Context(), req, &client, creds))

			resp, err := client.Do(req)
			r.NoError(err)
			r.NoError(resp.Body.Close())
			r.Equal(http.StatusOK, resp.StatusCode)
			if tc.wantAuth {
				r.NotEmpty(gotAuth)
			} else {
				r.Empty(gotAuth)
			}
		})
	}
}
