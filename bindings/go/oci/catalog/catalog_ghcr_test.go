package catalog

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	credsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
)

// ghcrTestServer serves the GitHub Packages REST API for one owner. It serves
// two package pages linked through the Link rel="next" header. It can answer
// the organization endpoint with HTTP 404 so a test can exercise the fallback
// to the user endpoint. It records the Authorization header of every request.
type ghcrTestServer struct {
	server      *httptest.Server
	mu          sync.Mutex
	authHeaders []string
	orgCalls    int
	userCalls   int
	orgNotFound bool
}

func newGHCRTestServer(t *testing.T, owner string, page1, page2 []ghcrPackage, orgNotFound bool) *ghcrTestServer {
	t.Helper()
	g := &ghcrTestServer{orgNotFound: orgNotFound}

	mux := http.NewServeMux()
	mux.HandleFunc("/orgs/"+owner+"/packages", func(w http.ResponseWriter, r *http.Request) {
		g.record(r, true)
		if g.orgNotFound {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		g.servePages(t, w, r, owner, page1, page2, "orgs")
	})
	mux.HandleFunc("/users/"+owner+"/packages", func(w http.ResponseWriter, r *http.Request) {
		g.record(r, false)
		g.servePages(t, w, r, owner, page1, page2, "users")
	})

	g.server = httptest.NewServer(mux)
	t.Cleanup(g.server.Close)
	return g
}

func (g *ghcrTestServer) record(r *http.Request, org bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.authHeaders = append(g.authHeaders, r.Header.Get("Authorization"))
	if org {
		g.orgCalls++
	} else {
		g.userCalls++
	}
}

func (g *ghcrTestServer) servePages(t *testing.T, w http.ResponseWriter, r *http.Request, owner string, page1, page2 []ghcrPackage, kind string) {
	t.Helper()
	if r.URL.Query().Get("page") == "2" {
		writeJSON(t, w, page2)
		return
	}
	next := "http://" + r.Host + "/" + kind + "/" + owner + "/packages?package_type=container&per_page=100&page=2"
	w.Header().Set("Link", "<"+next+`>; rel="next"`)
	writeJSON(t, w, page1)
}

// withGHCRBaseURL points the cataloger at the given base URL for the duration
// of the test.
func withGHCRBaseURL(t *testing.T, baseURL string) {
	t.Helper()
	previous := githubAPIBaseURL
	githubAPIBaseURL = baseURL
	t.Cleanup(func() { githubAPIBaseURL = previous })
}

func TestGHCRFactory_Match(t *testing.T) {
	f := ghcrFactory{}
	for _, host := range []string{"ghcr.io", "GHCR.IO"} {
		assert.Truef(t, f.Match(host), "expected match for %q", host)
	}
	for _, host := range []string{"docker.io", "quay.io", "example.com"} {
		assert.Falsef(t, f.Match(host), "expected no match for %q", host)
	}
}

func TestGHCRFactory_NewRejectsEmptyOwner(t *testing.T) {
	_, err := ghcrFactory{}.New(CatalogerOptions{Host: "ghcr.io", SubPath: ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "owner")
}

func TestGHCRCataloger_PaginatesAndAssembles(t *testing.T) {
	page1 := []ghcrPackage{
		{Name: "component-descriptors/componentB"},
		{Name: "not-a-component"},
	}
	page2 := []ghcrPackage{
		{Name: "component-descriptors/componentC"},
	}
	srv := newGHCRTestServer(t, "myorg", page1, page2, false)
	withGHCRBaseURL(t, srv.server.URL)

	cataloger, err := ghcrFactory{}.New(CatalogerOptions{
		Host:    "ghcr.io",
		SubPath: "myorg",
		Credentials: &credsv1.OCICredentials{
			AccessToken: "test-token",
		},
	})
	require.NoError(t, err)

	var repos []string
	err = cataloger.Repositories(t.Context(), "", func(page []string) error {
		repos = append(repos, page...)
		return nil
	})
	require.NoError(t, err)

	// Raw repository names are assembled as <owner>/<name> and pagination
	// follows the Link rel="next" header across both pages.
	assert.Equal(t, []string{
		"myorg/component-descriptors/componentB",
		"myorg/not-a-component",
		"myorg/component-descriptors/componentC",
	}, repos)

	srv.mu.Lock()
	defer srv.mu.Unlock()
	assert.Equal(t, 2, srv.orgCalls, "both pages should hit the org endpoint")
	assert.Equal(t, 0, srv.userCalls, "user endpoint should not be called for an org owner")
	require.NotEmpty(t, srv.authHeaders)
	for _, h := range srv.authHeaders {
		assert.Equal(t, "Bearer test-token", h)
	}
}

func TestGHCRCataloger_FallsBackToUserEndpoint(t *testing.T) {
	page1 := []ghcrPackage{
		{Name: "component-descriptors/componentA"},
	}
	page2 := []ghcrPackage{
		{Name: "component-descriptors/componentB"},
	}
	srv := newGHCRTestServer(t, "myuser", page1, page2, true)
	withGHCRBaseURL(t, srv.server.URL)

	cataloger, err := ghcrFactory{}.New(CatalogerOptions{
		Host:    "ghcr.io",
		SubPath: "myuser",
		Credentials: &credsv1.OCICredentials{
			Password: "pw-token",
		},
	})
	require.NoError(t, err)

	var repos []string
	err = cataloger.Repositories(t.Context(), "", func(page []string) error {
		repos = append(repos, page...)
		return nil
	})
	require.NoError(t, err)

	assert.Equal(t, []string{
		"myuser/component-descriptors/componentA",
		"myuser/component-descriptors/componentB",
	}, repos)

	srv.mu.Lock()
	defer srv.mu.Unlock()
	assert.Equal(t, 1, srv.orgCalls, "org endpoint should be tried once and return 404")
	assert.Equal(t, 2, srv.userCalls, "both pages should hit the user endpoint after fallback")
	for _, h := range srv.authHeaders {
		assert.Equal(t, "Bearer pw-token", h)
	}
}

func TestGHCRCataloger_MissingTokenIsUnauthenticated(t *testing.T) {
	cataloger, err := ghcrFactory{}.New(CatalogerOptions{Host: "ghcr.io", SubPath: "myorg"})
	require.NoError(t, err)

	err = cataloger.Repositories(t.Context(), "", func(page []string) error { return nil })
	require.ErrorIs(t, err, ErrCatalogUnauthenticated)
}

func TestGHCRCataloger_ListComponentsThroughLister(t *testing.T) {
	page1 := []ghcrPackage{
		{Name: "component-descriptors/componentB"},
		{Name: "component-descriptors/componentA"},
		{Name: "not-a-component"},
	}
	page2 := []ghcrPackage{
		{Name: "component-descriptors/componentC"},
	}
	srv := newGHCRTestServer(t, "myorg", page1, page2, false)
	withGHCRBaseURL(t, srv.server.URL)

	cataloger, err := ghcrFactory{}.New(CatalogerOptions{
		Host:    "ghcr.io",
		SubPath: "myorg",
		Credentials: &credsv1.OCICredentials{
			AccessToken: "test-token",
		},
	})
	require.NoError(t, err)

	lister := NewComponentLister(cataloger, "myorg")

	var names []string
	err = lister.ListComponents(t.Context(), "", func(found []string) error {
		names = append(names, found...)
		return nil
	})
	require.NoError(t, err)

	// Each page is deduplicated and sorted, then the pages concatenate. The
	// component-descriptors/ filter drops not-a-component and the subPath
	// prefix is stripped.
	assert.Equal(t, []string{"componentA", "componentB", "componentC"}, names)
}

func TestGHCRToken(t *testing.T) {
	assert.Equal(t, "", ghcrToken(nil))
	assert.Equal(t, "", ghcrToken(&credsv1.OCICredentials{}))
	assert.Equal(t, "at", ghcrToken(&credsv1.OCICredentials{AccessToken: "at", Password: "pw"}))
	assert.Equal(t, "pw", ghcrToken(&credsv1.OCICredentials{Password: "pw"}))
}

func TestGHCROwner(t *testing.T) {
	assert.Equal(t, "myorg", ghcrOwner("myorg"))
	assert.Equal(t, "myorg", ghcrOwner("/myorg/"))
	assert.Equal(t, "myorg", ghcrOwner("myorg/sub/path"))
	assert.Equal(t, "", ghcrOwner(""))
	assert.Equal(t, "", ghcrOwner("/"))
}

func TestNextLink(t *testing.T) {
	header := `<https://api.github.com/orgs/o/packages?page=2>; rel="next", <https://api.github.com/orgs/o/packages?page=5>; rel="last"`
	assert.Equal(t, "https://api.github.com/orgs/o/packages?page=2", nextLink(header))

	last := `<https://api.github.com/orgs/o/packages?page=1>; rel="prev"`
	assert.Equal(t, "", nextLink(last))

	assert.Equal(t, "", nextLink(""))
}
