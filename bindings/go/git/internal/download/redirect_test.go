package download

import (
	"context"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	gitclient "github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/stretchr/testify/require"

	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
)

func installRedirectTestClient(t *testing.T, client *http.Client) {
	t.Helper()
	previousHTTP, previousHTTPS := gitclient.Protocols["http"], gitclient.Protocols["https"]
	t.Cleanup(func() {
		gitclient.Protocols["http"], gitclient.Protocols["https"] = previousHTTP, previousHTTPS
	})
	if client != nil {
		InstallHTTPClient(client)
	}
}

func TestDownloadHTTPSRedirectDoesNotLeakCredentials(t *testing.T) {
	for _, clientName := range []string{"default", "configured", "configured-callback"} {
		for _, ref := range []string{"HEAD", "refs/heads/main"} {
			for _, tc := range []struct {
				name  string
				creds credsv1.GitCredentials
			}{
				{name: "token", creds: credsv1.GitCredentials{Token: "redirect-test-token"}},
				{name: "basic", creds: credsv1.GitCredentials{Username: "redirect-test-user", Password: "redirect-test-password"}},
			} {
				t.Run(clientName+"/"+ref+"/"+tc.name, func(t *testing.T) {
					r := require.New(t)

					// Retain only booleans so failures never print credential values.
					var authenticatedHTTPSRequest, httpRequest, callbackCalled atomic.Bool
					plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
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
						http.Redirect(w, req, plain.URL+req.URL.RequestURI(), http.StatusFound)
					}))
					t.Cleanup(secure.Close)

					secureURL, err := url.Parse(secure.URL)
					r.NoError(err)
					plainURL, err := url.Parse(plain.URL)
					r.NoError(err)
					r.Equal(secureURL.Hostname(), plainURL.Hostname(), "redirect must stay on the same hostname")

					opts := Options{TempDir: t.TempDir()}
					var client *http.Client
					if clientName == "default" {
						opts.CABundle = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: secure.Certificate().Raw})
					} else {
						client = secure.Client()
						if clientName == "configured-callback" {
							client.CheckRedirect = func(*http.Request, []*http.Request) error {
								callbackCalled.Store(true)
								return nil
							}
						}
					}
					installRedirectTestClient(t, client)

					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					defer cancel()
					_, err = Download(ctx, &accessv1.Git{Repository: secure.URL + "/repo.git", Ref: ref}, &tc.creds, opts)

					r.True(authenticatedHTTPSRequest.Load(), "Download must authenticate the HTTPS Git discovery request before the redirect")
					r.False(httpRequest.Load(), "Download must not make any HTTP request on an HTTPS-to-HTTP redirect")
					r.ErrorContains(err, "redirect from HTTPS to HTTP is not allowed")
					r.False(callbackCalled.Load(), "the downgrade guard must run before the configured callback")
				})
			}
		}
	}
}

func TestDownloadRedirectLimit(t *testing.T) {
	callbackErr := errors.New("configured redirect limit reached")
	for _, tc := range []struct {
		name         string
		client       *http.Client
		wantError    string
		wantRequests int32
	}{
		{name: "default", wantError: "http redirect: too many redirects", wantRequests: 10},
		{name: "configured without callback", client: &http.Client{}, wantError: "http redirect: too many redirects", wantRequests: 10},
		{
			name:      "permissive callback retains transport limit",
			client:    &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return nil }},
			wantError: "http redirect: too many redirects", wantRequests: 10,
		},
		{
			name: "configured callback imposes stricter limit",
			client: &http.Client{CheckRedirect: func(_ *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return callbackErr
				}
				return nil
			}},
			wantError: callbackErr.Error(), wantRequests: 3,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				requests.Add(1)
				http.Redirect(w, req, req.URL.RequestURI(), http.StatusFound)
			}))
			t.Cleanup(server.Close)
			installRedirectTestClient(t, tc.client)

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			_, err := Download(ctx, &accessv1.Git{Repository: server.URL + "/repo.git", Ref: "HEAD"}, nil, Options{TempDir: t.TempDir()})
			r.ErrorContains(err, tc.wantError)
			r.Equal(tc.wantRequests, requests.Load())
		})
	}
}

func TestDownloadAllowedRedirects(t *testing.T) {
	callbackErr := errors.New("redirect rejected by configured callback")
	for _, clientName := range []string{"default", "configured", "configured-reject"} {
		for _, scheme := range []string{"https", "http"} {
			t.Run(clientName+"/"+scheme+"-to-https", func(t *testing.T) {
				r := require.New(t)
				var targetReached, callbackCalled atomic.Bool
				reject := clientName == "configured-reject"
				secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					if req.URL.Path == "/target.git/info/refs" && req.URL.Query().Get("service") == "git-upload-pack" {
						targetReached.Store(true)
					}
					w.WriteHeader(http.StatusNotFound)
				}))
				t.Cleanup(secure.Close)
				handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					http.Redirect(w, req, secure.URL+"/target.git/info/refs?service=git-upload-pack", http.StatusFound)
				})
				var source *httptest.Server
				if scheme == "https" {
					source = httptest.NewTLSServer(handler)
				} else {
					source = httptest.NewServer(handler)
				}
				t.Cleanup(source.Close)

				opts := Options{TempDir: t.TempDir()}
				var client *http.Client
				if clientName == "default" {
					opts.CABundle = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: secure.Certificate().Raw})
				} else {
					client = secure.Client()
					client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
						callbackCalled.Store(req.URL.Path == "/target.git/info/refs" && len(via) == 1 && via[0].URL.Path == "/repo.git/info/refs")
						if reject {
							return callbackErr
						}
						return nil
					}
				}
				installRedirectTestClient(t, client)
				if client != nil {
					// Mutating the caller's client must not change the installed clone.
					client.CheckRedirect = nil
				}
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				_, err := Download(ctx, &accessv1.Git{Repository: source.URL + "/repo.git", Ref: "HEAD"}, nil, opts)

				r.Equal(client != nil, callbackCalled.Load(), "the original callback must receive the redirect and its history")
				r.Equal(!reject, targetReached.Load(), "only the configured callback may block the allowed redirect")
				if reject {
					r.ErrorContains(err, callbackErr.Error())
				} else {
					r.Error(err, "the redirect target is not a Git repository")
					r.NotContains(err.Error(), "redirect from HTTPS to HTTP is not allowed")
				}
			})
		}
	}
}
