package download

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
)

func TestDownloadHTTPSRedirectDoesNotLeakCredentials(t *testing.T) {
	for _, tc := range []struct {
		name  string
		creds credsv1.GitCredentials
	}{
		{name: "token", creds: credsv1.GitCredentials{Token: "redirect-test-token"}},
		{name: "basic", creds: credsv1.GitCredentials{Username: "redirect-test-user", Password: "redirect-test-password"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			// Retain only booleans so failures never print credential values.
			var authenticatedHTTPSRequest, credentialsOverHTTP atomic.Bool
			plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Header.Get("Authorization") != "" {
					credentialsOverHTTP.Store(true)
				}
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
				http.Redirect(w, req, plain.URL+req.URL.RequestURI(), http.StatusFound)
			}))
			t.Cleanup(secure.Close)

			secureURL, err := url.Parse(secure.URL)
			r.NoError(err)
			plainURL, err := url.Parse(plain.URL)
			r.NoError(err)
			r.Equal(secureURL.Hostname(), plainURL.Hostname(), "redirect must stay on the same hostname")

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			_, err = Download(ctx, &accessv1.Git{Repository: secure.URL + "/repo.git", Ref: "HEAD"}, &tc.creds, Options{
				TempDir: t.TempDir(),
				CABundle: pem.EncodeToMemory(&pem.Block{
					Type: "CERTIFICATE", Bytes: secure.Certificate().Raw,
				}),
			})

			r.True(authenticatedHTTPSRequest.Load(), "Download must authenticate the HTTPS Git discovery request before the redirect")
			// Rejecting the downgrade and following it without credentials are both safe.
			r.False(credentialsOverHTTP.Load(), "Download forwarded credentials on a same-host HTTPS-to-HTTP redirect")
			r.Error(err, "the redirect target is not a Git repository")
		})
	}
}
