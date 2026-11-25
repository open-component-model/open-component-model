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

// quayTestServer serves the Quay REST API for one namespace. It serves two
// repository pages linked through the next_page token. It records the
// Authorization header and the next_page query of every request so a test can
// assert pagination and authentication.
type quayTestServer struct {
	server      *httptest.Server
	nextToken   string
	mu          sync.Mutex
	authHeaders []string
	nextQueries []string
}

func newQuayTestServer(t *testing.T, namespace string, page1, page2 []quayRepository) *quayTestServer {
	t.Helper()
	q := &quayTestServer{nextToken: "page-2-token"}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/repository", func(w http.ResponseWriter, r *http.Request) {
		q.mu.Lock()
		q.authHeaders = append(q.authHeaders, r.Header.Get("Authorization"))
		q.nextQueries = append(q.nextQueries, r.URL.Query().Get("next_page"))
		q.mu.Unlock()

		assert.Equal(t, namespace, r.URL.Query().Get("namespace"))

		if r.URL.Query().Get("next_page") == q.nextToken {
			writeJSON(t, w, quayPage{Repositories: page2})
			return
		}

		writeJSON(t, w, quayPage{Repositories: page1, NextPage: q.nextToken})
	})

	q.server = httptest.NewServer(mux)
	t.Cleanup(q.server.Close)
	return q
}

// withQuayBaseURL points the cataloger at the given base URL for the duration
// of the test.
func withQuayBaseURL(t *testing.T, baseURL string) {
	t.Helper()
	previous := quayAPIBaseURL
	quayAPIBaseURL = baseURL
	t.Cleanup(func() { quayAPIBaseURL = previous })
}

func TestQuayFactory_Match(t *testing.T) {
	f := quayFactory{}
	for _, host := range []string{"quay.io", "QUAY.IO", "enterprise.quay.io"} {
		assert.Truef(t, f.Match(host), "expected match for %q", host)
	}
	for _, host := range []string{"docker.io", "ghcr.io", "example.com", "notquay.io"} {
		assert.Falsef(t, f.Match(host), "expected no match for %q", host)
	}
}

func TestQuayFactory_NewRejectsEmptyNamespace(t *testing.T) {
	_, err := quayFactory{}.New(CatalogerOptions{Host: "quay.io", SubPath: ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace")
}

func TestQuayCataloger_PaginatesAndAssembles(t *testing.T) {
	page1 := []quayRepository{
		{Namespace: "myorg", Name: "component-descriptors/componentB"},
		{Namespace: "myorg", Name: "not-a-component"},
	}
	page2 := []quayRepository{
		{Namespace: "", Name: "component-descriptors/componentC"},
	}
	srv := newQuayTestServer(t, "myorg", page1, page2)
	withQuayBaseURL(t, srv.server.URL)

	cataloger, err := quayFactory{}.New(CatalogerOptions{Host: "quay.io", SubPath: "myorg"})
	require.NoError(t, err)

	var repos []string
	err = cataloger.Repositories(t.Context(), "", func(page []string) error {
		repos = append(repos, page...)
		return nil
	})
	require.NoError(t, err)

	// Raw repository names are assembled as <namespace>/<name>. The second
	// page carries an empty namespace, so the cataloger falls back to the
	// configured namespace. Pagination follows next_page across both pages.
	assert.Equal(t, []string{
		"myorg/component-descriptors/componentB",
		"myorg/not-a-component",
		"myorg/component-descriptors/componentC",
	}, repos)

	srv.mu.Lock()
	defer srv.mu.Unlock()
	// The first request carries no next_page token and the second request
	// carries the token returned by the first page.
	assert.Equal(t, []string{"", "page-2-token"}, srv.nextQueries)
}

func TestQuayCataloger_ListComponentsThroughLister(t *testing.T) {
	page1 := []quayRepository{
		{Namespace: "myorg", Name: "component-descriptors/componentB"},
		{Namespace: "myorg", Name: "component-descriptors/componentA"},
		{Namespace: "myorg", Name: "not-a-component"},
	}
	page2 := []quayRepository{
		{Namespace: "myorg", Name: "component-descriptors/componentC"},
	}
	srv := newQuayTestServer(t, "myorg", page1, page2)
	withQuayBaseURL(t, srv.server.URL)

	cataloger, err := quayFactory{}.New(CatalogerOptions{Host: "quay.io", SubPath: "myorg"})
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

func TestQuayCataloger_AuthenticatesWithBearer(t *testing.T) {
	page1 := []quayRepository{{Namespace: "myorg", Name: "component-descriptors/componentA"}}
	srv := newQuayTestServer(t, "myorg", page1, nil)
	withQuayBaseURL(t, srv.server.URL)

	cataloger, err := quayFactory{}.New(CatalogerOptions{
		Host:    "quay.io",
		SubPath: "myorg",
		Credentials: &credsv1.OCICredentials{
			AccessToken: "test-token",
		},
	})
	require.NoError(t, err)

	err = cataloger.Repositories(t.Context(), "", func(page []string) error { return nil })
	require.NoError(t, err)

	srv.mu.Lock()
	defer srv.mu.Unlock()
	require.NotEmpty(t, srv.authHeaders)
	for _, h := range srv.authHeaders {
		assert.Equal(t, "Bearer test-token", h)
	}
}

func TestQuayCataloger_NoAuthWithoutCredentials(t *testing.T) {
	page1 := []quayRepository{{Namespace: "myorg", Name: "component-descriptors/componentA"}}
	srv := newQuayTestServer(t, "myorg", page1, nil)
	withQuayBaseURL(t, srv.server.URL)

	cataloger, err := quayFactory{}.New(CatalogerOptions{Host: "quay.io", SubPath: "myorg"})
	require.NoError(t, err)

	err = cataloger.Repositories(t.Context(), "", func(page []string) error { return nil })
	require.NoError(t, err)

	srv.mu.Lock()
	defer srv.mu.Unlock()
	require.NotEmpty(t, srv.authHeaders)
	for _, h := range srv.authHeaders {
		assert.Empty(t, h, "no Authorization header should be sent without credentials")
	}
}

func TestQuayToken(t *testing.T) {
	assert.Equal(t, "", quayToken(nil))
	assert.Equal(t, "", quayToken(&credsv1.OCICredentials{}))
	assert.Equal(t, "at", quayToken(&credsv1.OCICredentials{AccessToken: "at", Password: "pw"}))
	assert.Equal(t, "pw", quayToken(&credsv1.OCICredentials{Password: "pw"}))
}

func TestQuayNamespace(t *testing.T) {
	assert.Equal(t, "myorg", quayNamespace("myorg"))
	assert.Equal(t, "myorg", quayNamespace("/myorg/"))
	assert.Equal(t, "myorg", quayNamespace("myorg/sub/path"))
	assert.Equal(t, "", quayNamespace(""))
	assert.Equal(t, "", quayNamespace("/"))
}
