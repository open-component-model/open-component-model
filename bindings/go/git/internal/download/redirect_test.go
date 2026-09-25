package download

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
)

func TestDownloadHTTPSRedirectDoesNotLeakCredentials(t *testing.T) {
	for _, ref := range []string{"HEAD", "refs/heads/main"} {
		for _, tc := range []struct {
			name  string
			creds credsv1.GitCredentials
		}{
			{name: "token", creds: credsv1.GitCredentials{Token: "redirect-test-token"}},
			{name: "basic", creds: credsv1.GitCredentials{Username: "redirect-test-user", Password: "redirect-test-password"}},
		} {
			t.Run(ref+"/"+tc.name, func(t *testing.T) {
				r := require.New(t)

				// Retain only booleans so failures never print credential values.
				var authenticatedHTTPSRequest, httpRequest atomic.Bool
				plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					httpRequest.Store(true)
					w.WriteHeader(http.StatusNotFound)
				}))
				t.Cleanup(plain.Close)

				secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					authenticated := false
					if tc.creds.Token != "" {
						authenticated = req.Header.Get("Authorization") == "Bearer "+tc.creds.Token
					} else {
						username, password, ok := req.BasicAuth()
						authenticated = ok && username == tc.creds.Username && password == tc.creds.Password
					}
					if authenticated && req.URL.Path == "/repo.git/info/refs" && req.URL.Query().Get("service") == "git-upload-pack" {
						authenticatedHTTPSRequest.Store(true)
					}
					// Same host, so the standard library alone would keep the credentials.
					http.Redirect(w, req, plain.URL+req.URL.RequestURI(), http.StatusFound)
				}))
				t.Cleanup(secure.Close)

				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				_, err := Download(ctx, &accessv1.Git{Repository: secure.URL + "/repo.git", Ref: ref}, &tc.creds,
					Options{TempDir: t.TempDir(), HTTPClient: secure.Client()})

				r.True(authenticatedHTTPSRequest.Load(), "Download must authenticate the HTTPS Git discovery request before the redirect")
				r.False(httpRequest.Load(), "Download must not make any HTTP request on an HTTPS-to-HTTP redirect")
				r.ErrorContains(err, "redirect downgrades scheme")
			})
		}
	}
}
