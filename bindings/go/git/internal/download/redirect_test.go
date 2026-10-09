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
			{name: "explicit HTTPS", creds: &credsv1.GitHTTPSCredentials{Username: "redirect-test-user", Password: "redirect-test-token"}, authorization: "Basic " + base64.StdEncoding.EncodeToString([]byte("redirect-test-user:redirect-test-token"))},
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

type redirectTestTransport struct {
	target    string
	transport http.RoundTripper
}

func (tr redirectTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Host = req.URL.Host
	clone.URL.Host = tr.target
	return tr.transport.RoundTrip(clone)
}

func TestDownloadAuthenticatedRedirectOrigins(t *testing.T) {
	for _, ref := range []string{"HEAD", "refs/heads/main"} {
		for _, target := range []struct {
			name, host, path string
			sameOrigin       bool
		}{
			{"child subdomain", "child.git.example.test", "/repo.git", false},
			{"other hostname", "other.example.test", "/repo.git", false},
			{"other port", "git.example.test:8443", "/repo.git", false},
			{"same origin path", "git.example.test", "/canonical/repo.git", true},
		} {
			for _, auth := range []struct {
				name          string
				creds         runtime.Typed
				userinfo      string
				authenticated bool
			}{
				{name: "HTTPS", creds: &credsv1.GitHTTPSCredentials{Username: "user", Password: "example-token"}, authenticated: true},
				{name: "bearer", creds: &credsv1.GitBearerCredentials{Token: "example-token"}, authenticated: true},
				{name: "URL userinfo", userinfo: "user:example-token@", authenticated: true},
				{name: "anonymous"},
			} {
				t.Run(ref+"/"+target.name+"/"+auth.name, func(t *testing.T) {
					r := require.New(t)
					var initialAuthenticated, targetReached, targetAuthenticated atomic.Bool
					secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
						authenticated := req.Header.Get("Authorization") != ""
						if req.Host == "git.example.test" && req.URL.Path == "/repo.git/info/refs" {
							initialAuthenticated.Store(authenticated)
							http.Redirect(w, req, "https://"+target.host+target.path+"/info/refs?service=git-upload-pack", http.StatusFound)
							return
						}
						targetReached.Store(true)
						targetAuthenticated.Store(authenticated)
						w.WriteHeader(http.StatusNotFound)
					}))
					t.Cleanup(secure.Close)
					httpClient := secure.Client()
					httpClient.Transport = redirectTestTransport{target: secure.Listener.Addr().String(), transport: httpClient.Transport}
					_, err := Download(t.Context(), &accessv1.Git{Repository: "https://" + auth.userinfo + "git.example.test/repo.git", Ref: ref}, auth.creds, Options{TempDir: t.TempDir(), HTTPClient: httpClient})
					r.Error(err)
					r.Equal(auth.authenticated, initialAuthenticated.Load())
					if auth.authenticated && !target.sameOrigin {
						r.ErrorContains(err, "authenticated git redirect changes origin")
						r.False(targetReached.Load())
						r.False(targetAuthenticated.Load())
					} else {
						r.True(targetReached.Load())
						r.Equal(auth.authenticated, targetAuthenticated.Load())
					}
				})
			}
		}
	}
}
