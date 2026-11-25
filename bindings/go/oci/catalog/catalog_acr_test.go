package catalog

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// acrTestServer serves the ACR /acr/v1/_catalog endpoint for one registry. It
// serves two repository pages linked through a Link rel="next" header. It
// records the last query of every request so a test can assert pagination.
type acrTestServer struct {
	server     *httptest.Server
	mu         sync.Mutex
	lastValues []string
}

func newACRTestServer(t *testing.T, page1, page2 []string, page1Last string) *acrTestServer {
	t.Helper()
	a := &acrTestServer{}

	mux := http.NewServeMux()
	mux.HandleFunc("/acr/v1/_catalog", func(w http.ResponseWriter, r *http.Request) {
		last := r.URL.Query().Get("last")
		a.mu.Lock()
		a.lastValues = append(a.lastValues, last)
		a.mu.Unlock()

		if last == page1Last {
			writeJSON(t, w, acrPage{Repositories: page2})
			return
		}

		w.Header().Set("Link", "</acr/v1/_catalog?last="+page1Last+"&n=100>; rel=\"next\"")
		writeJSON(t, w, acrPage{Repositories: page1})
	})

	a.server = httptest.NewServer(mux)
	t.Cleanup(a.server.Close)
	return a
}

// acrHost returns the host of the test server, without the http:// scheme, so
// it can be used as the CatalogerOptions host.
func (a *acrTestServer) host() string {
	return strings.TrimPrefix(a.server.URL, "http://")
}

func TestACRFactory_Match(t *testing.T) {
	f := acrFactory{}
	for _, host := range []string{"myregistry.azurecr.io", "MyRegistry.AzureCR.io", "foo.bar.azurecr.io"} {
		assert.Truef(t, f.Match(host), "expected match for %q", host)
	}
	for _, host := range []string{"azurecr.io", "docker.io", "ghcr.io", "example.com", "azurecr.io.evil.com"} {
		assert.Falsef(t, f.Match(host), "expected no match for %q", host)
	}
}

func TestACRCataloger_PaginatesAndAssembles(t *testing.T) {
	page1 := []string{"component-descriptors/componentB", "not-a-component"}
	page2 := []string{"component-descriptors/componentC"}
	srv := newACRTestServer(t, page1, page2, "component-descriptors/componentB")

	cataloger, err := acrFactory{}.New(CatalogerOptions{
		Host:       srv.host(),
		HTTPClient: srv.server.Client(),
		PlainHTTP:  true,
	})
	require.NoError(t, err)

	var repos []string
	err = cataloger.Repositories(t.Context(), "", func(page []string) error {
		repos = append(repos, page...)
		return nil
	})
	require.NoError(t, err)

	// The raw repository names concatenate across both pages. Pagination
	// follows the Link rel="next" header.
	assert.Equal(t, []string{
		"component-descriptors/componentB",
		"not-a-component",
		"component-descriptors/componentC",
	}, repos)

	srv.mu.Lock()
	defer srv.mu.Unlock()
	// The first request carries no last cursor and the second request carries
	// the last repository of the first page from the Link header.
	assert.Equal(t, []string{"", "component-descriptors/componentB"}, srv.lastValues)
}

func TestACRCataloger_ListComponentsThroughLister(t *testing.T) {
	page1 := []string{
		"myteam/component-descriptors/componentB",
		"myteam/component-descriptors/componentA",
		"myteam/not-a-component",
		"other/component-descriptors/componentX",
	}
	page2 := []string{"myteam/component-descriptors/componentC"}
	srv := newACRTestServer(t, page1, page2, "myteam/component-descriptors/componentB")

	cataloger, err := acrFactory{}.New(CatalogerOptions{
		Host:       srv.host(),
		SubPath:    "myteam",
		HTTPClient: srv.server.Client(),
		PlainHTTP:  true,
	})
	require.NoError(t, err)

	lister := NewComponentLister(cataloger, "myteam")

	var names []string
	err = lister.ListComponents(t.Context(), "", func(found []string) error {
		names = append(names, found...)
		return nil
	})
	require.NoError(t, err)

	// Each page is deduplicated and sorted, then the pages concatenate. The
	// component-descriptors/ filter drops not-a-component, the subPath prefix
	// scopes the match to myteam and is stripped, and the other/ repository is
	// dropped because it does not match the subPath.
	assert.Equal(t, []string{"componentA", "componentB", "componentC"}, names)
}

// acrFallbackServer serves both the ACR endpoint and the standard Distribution
// endpoint. The ACR endpoint answers with HTTP 404 so the cataloger falls back
// to the Distribution /v2/_catalog endpoint.
func TestACRCataloger_FallsBackToDistribution(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/acr/v1/_catalog", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	// ORAS pings /v2/ before listing. Answer it with 200 so the client treats
	// the registry as a Distribution registry.
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/_catalog" {
			writeJSON(t, w, struct {
				Repositories []string `json:"repositories"`
			}{Repositories: []string{"component-descriptors/componentA", "not-a-component"}})
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	host := strings.TrimPrefix(server.URL, "http://")

	cataloger, err := acrFactory{}.New(CatalogerOptions{
		Host:       host,
		HTTPClient: server.Client(),
		PlainHTTP:  true,
	})
	require.NoError(t, err)

	var repos []string
	err = cataloger.Repositories(t.Context(), "", func(page []string) error {
		repos = append(repos, page...)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"component-descriptors/componentA", "not-a-component"}, repos)
}

func TestACRNextLink(t *testing.T) {
	base := "https://myregistry.azurecr.io/acr/v1/_catalog?n=100"
	assert.Equal(t,
		"https://myregistry.azurecr.io/acr/v1/_catalog?last=x&n=100",
		acrNextLink(`</acr/v1/_catalog?last=x&n=100>; rel="next"`, base))
	assert.Equal(t, "", acrNextLink(`</acr/v1/_catalog?last=x>; rel="prev"`, base))
	assert.Equal(t, "", acrNextLink("", base))
}
