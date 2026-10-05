package download

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestDownloadHTTPSRedirectDoesNotLeakCredentials(t *testing.T) {
	for _, ref := range []string{"HEAD", "refs/heads/main"} {
		for _, tc := range []struct {
			name          string
			creds         runtime.Typed
			authorization string
		}{
			{name: "legacy token", creds: &credsv1.GitCredentials{Token: "redirect-test-token"}, authorization: "Bearer redirect-test-token"},
			{name: "explicit bearer", creds: &credsv1.GitBearerCredentials{Token: "redirect-test-token"}, authorization: "Bearer redirect-test-token"},
			{name: "explicit basic", creds: &credsv1.GitBasicCredentials{Username: "redirect-test-user", Password: "redirect-test-token"}, authorization: "Basic " + base64.StdEncoding.EncodeToString([]byte("redirect-test-user:redirect-test-token"))},
			{name: "legacy basic", creds: &credsv1.GitCredentials{Username: "redirect-test-user", Password: "redirect-test-password"}, authorization: "Basic " + base64.StdEncoding.EncodeToString([]byte("redirect-test-user:redirect-test-password"))},
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
					authenticated := req.Header.Get("Authorization") == tc.authorization
					if authenticated && req.URL.Path == "/repo.git/info/refs" && req.URL.Query().Get("service") == "git-upload-pack" {
						authenticatedHTTPSRequest.Store(true)
					}
					// Same host, so the standard library alone would keep the credentials.
					http.Redirect(w, req, plain.URL+req.URL.RequestURI(), http.StatusFound)
				}))
				t.Cleanup(secure.Close)

				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				credentials := tc.creds
				if legacy, ok := credentials.(*credsv1.GitCredentials); ok {
					legacy.Type = runtime.NewVersionedType(credsv1.GitCredentialsType, credsv1.Version)
					var err error
					credentials, err = credsv1.ConvertCredentials(legacy)
					r.NoError(err)
				}
				_, err := Download(ctx, &accessv1.Git{Repository: secure.URL + "/repo.git", Ref: ref}, credentials,
					Options{TempDir: t.TempDir(), HTTPClient: secure.Client()})

				r.True(authenticatedHTTPSRequest.Load(), "Download must authenticate the HTTPS Git discovery request before the redirect")
				r.False(httpRequest.Load(), "Download must not make any HTTP request on an HTTPS-to-HTTP redirect")
				r.ErrorContains(err, "redirect downgrades scheme")
			})
		}
	}
}
