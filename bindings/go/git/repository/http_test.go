package repository_test

import (
	"context"

	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	gitclient "github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/git/repository"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func preserveHTTPProtocols(t *testing.T) {
	t.Helper()
	previous := gitclient.Protocols
	gitclient.Protocols = maps.Clone(previous)
	t.Cleanup(func() { gitclient.Protocols = previous })
}

func TestNewResourceRepositoryInstallsDefaultHTTPClient(t *testing.T) {
	r := require.New(t)
	preserveHTTPProtocols(t)

	previous := githttp.NewClient(&http.Client{})
	gitclient.InstallProtocol("http", previous)
	gitclient.InstallProtocol("https", previous)

	repository.NewResourceRepository(nil)

	r.NotNil(gitclient.Protocols["http"])
	r.NotSame(previous, gitclient.Protocols["http"])
	r.NotSame(previous, gitclient.Protocols["https"])
	r.Same(gitclient.Protocols["http"], gitclient.Protocols["https"])
}

func TestResourceRepositoryHTTPConfigRejectsDowngrade(t *testing.T) {
	for _, configName := range []string{"default", "configured", "per-host"} {
		for _, ref := range []string{"HEAD", "refs/heads/main"} {
			t.Run(configName+"/"+ref, func(t *testing.T) {
				r := require.New(t)
				preserveHTTPProtocols(t)

				var authenticatedDiscovery, httpReached atomic.Bool
				plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					httpReached.Store(true)
					w.WriteHeader(http.StatusNotFound)
				}))
				t.Cleanup(plain.Close)

				creds := &credsv1.GitCredentials{
					Type:  runtime.NewVersionedType("GitCredentials", "v1"),
					Token: "repository-redirect-test-token",
				}
				secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					if req.Method == http.MethodGet && req.URL.Path == "/repo.git/info/refs" &&
						req.URL.Query().Get("service") == "git-upload-pack" &&
						req.Header.Get("Authorization") == "Bearer "+creds.Token {
						authenticatedDiscovery.Store(true)
					}
					http.Redirect(w, req, plain.URL+req.URL.RequestURI(), http.StatusFound)
				}))
				t.Cleanup(secure.Close)

				secureURL, err := url.Parse(secure.URL)
				r.NoError(err)
				plainURL, err := url.Parse(plain.URL)
				r.NoError(err)
				r.Equal(secureURL.Hostname(), plainURL.Hostname(), "same-host redirects could forward credentials")

				previousTransport := http.DefaultTransport
				http.DefaultTransport = secure.Client().Transport
				t.Cleanup(func() { http.DefaultTransport = previousTransport })

				var opts []repository.Option
				if configName != "default" {
					cfg := &httpv1alpha1.Config{
						TimeoutConfig: httpv1alpha1.TimeoutConfig{Timeout: httpv1alpha1.NewTimeout(5 * time.Second)},
					}
					if configName == "per-host" {
						cfg.Hosts = map[string]*httpv1alpha1.HostConfig{
							secureURL.Host: {TimeoutConfig: httpv1alpha1.TimeoutConfig{
								ResponseHeaderTimeout: httpv1alpha1.NewTimeout(time.Second),
							}},
						}
					}
					opts = append(opts, repository.WithHTTPConfig(cfg))
				}
				dir := t.TempDir()
				repo := repository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &dir}, opts...)
				res := &descriptor.Resource{Access: &accessv1.Git{
					Type: runtime.NewVersionedType("Git", "v1"), Repository: secure.URL + "/repo.git", Ref: ref,
				}}
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()

				b, err := repo.DownloadResource(ctx, res, creds)

				r.ErrorContains(err, "redirect from HTTPS to HTTP is not allowed")
				r.Nil(b)
				r.True(authenticatedDiscovery.Load(), "authenticated HTTPS discovery must succeed with TLS verification enabled")
				r.False(httpReached.Load(), "the downgrade must be rejected before any HTTP request reaches the target")
			})
		}
	}
}
