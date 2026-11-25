package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"golang.org/x/oauth2"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	credsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
)

// garCatalogTestServer serves the Distribution catalog endpoint for the GAR
// tests. It records the Authorization header of every catalog request so a
// test can assert that the Google access token is forwarded as a Bearer token.
type garCatalogTestServer struct {
	server      *httptest.Server
	mu          sync.Mutex
	authHeaders []string
}

func newGARCatalogTestServer(t *testing.T, page1, page2 []string) *garCatalogTestServer {
	t.Helper()
	s := &garCatalogTestServer{}

	mux := http.NewServeMux()
	mux.HandleFunc("/v2/_catalog", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.authHeaders = append(s.authHeaders, r.Header.Get("Authorization"))
		s.mu.Unlock()

		if r.URL.Query().Get("last") != "" {
			writeJSON(t, w, struct {
				Repositories []string `json:"repositories"`
			}{Repositories: page2})
			return
		}

		if len(page2) > 0 {
			w.Header().Set("Link", `</v2/_catalog?last=`+page1[len(page1)-1]+`>; rel="next"`)
		}
		writeJSON(t, w, struct {
			Repositories []string `json:"repositories"`
		}{Repositories: page1})
	})

	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)
	return s
}

// withGARTokenSource injects a static Google token source for the duration of
// the test, so no test reaches Google.
func withGARTokenSource(t *testing.T, token string) {
	t.Helper()
	previous := garDefaultTokenSource
	garDefaultTokenSource = func(_ context.Context, _ ...string) (oauth2.TokenSource, error) {
		return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}), nil
	}
	t.Cleanup(func() { garDefaultTokenSource = previous })
}

func TestGARFactory_Match(t *testing.T) {
	f := garFactory{}
	for _, host := range []string{
		"europe-docker.pkg.dev",
		"us-central1-docker.pkg.dev",
		"gcr.io",
		"eu.gcr.io",
		"GCR.IO",
	} {
		assert.Truef(t, f.Match(host), "expected match for %q", host)
	}
	for _, host := range []string{
		"ghcr.io",
		"docker.io",
		"pkg.dev",
		"example-docker.pkg.dev.evil.com",
		"gcr.io.evil.com",
	} {
		assert.Falsef(t, f.Match(host), "expected no match for %q", host)
	}
}

func TestGARCataloger_ForwardsBearerTokenAndPaginates(t *testing.T) {
	withGARTokenSource(t, "test-google-token")
	srv := newGARCatalogTestServer(t,
		[]string{"component-descriptors/componentB", "not-a-component"},
		[]string{"component-descriptors/componentC"},
	)

	host := strings.TrimPrefix(srv.server.URL, "http://")

	cataloger, err := garFactory{}.New(CatalogerOptions{
		Host:       host,
		HTTPClient: srv.server.Client(),
		PlainHTTP:  true,
	})
	require.NoError(t, err)

	lister := NewComponentLister(cataloger, "")

	var names []string
	err = lister.ListComponents(t.Context(), "", func(found []string) error {
		names = append(names, found...)
		return nil
	})
	require.NoError(t, err)

	// The component-descriptors/ filter keeps the two components across both
	// pages and drops not-a-component.
	assert.Equal(t, []string{"componentB", "componentC"}, names)

	// Every catalog request carries the Google access token as a Bearer token.
	srv.mu.Lock()
	defer srv.mu.Unlock()
	require.NotEmpty(t, srv.authHeaders)
	for _, header := range srv.authHeaders {
		assert.Equal(t, "Bearer test-google-token", header)
	}
}

func TestGARFactory_UsesExplicitCredentials(t *testing.T) {
	// When explicit credentials are present, the factory returns the standard
	// Distribution cataloger instead of the Google ADC cataloger.
	cataloger, err := garFactory{}.New(CatalogerOptions{
		Host:        "europe-docker.pkg.dev",
		Credentials: &credsv1.OCICredentials{Username: "oauth2accesstoken", Password: "token"},
	})
	require.NoError(t, err)
	_, ok := cataloger.(*DistributionCataloger)
	assert.True(t, ok, "expected the distribution cataloger for explicit credentials")
}
