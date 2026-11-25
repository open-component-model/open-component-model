package catalog

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	credsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
)

// dockerHubTestServer serves the Docker Hub web API for one namespace. It
// serves two repository pages linked through next and an optional login
// endpoint. It records the Authorization header of every repository request so
// a test can assert that the JWT is sent after login.
type dockerHubTestServer struct {
	server      *httptest.Server
	token       string
	mu          sync.Mutex
	authHeaders []string
	loginCalls  int
}

func newDockerHubTestServer(t *testing.T, namespace string, page1, page2 []dockerHubRepository) *dockerHubTestServer {
	t.Helper()
	d := &dockerHubTestServer{token: "test-jwt-token"}

	mux := http.NewServeMux()
	mux.HandleFunc("/v2/users/login", func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		d.loginCalls++
		d.mu.Unlock()
		writeJSON(t, w, dockerHubLoginResponse{Token: d.token})
	})
	mux.HandleFunc("/v2/repositories/"+namespace+"/", func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		d.authHeaders = append(d.authHeaders, r.Header.Get("Authorization"))
		d.mu.Unlock()

		if r.URL.Query().Get("page") == "2" {
			writeJSON(t, w, dockerHubPage{Count: len(page1) + len(page2), Results: page2})
			return
		}

		next := "http://" + r.Host + "/v2/repositories/" + namespace + "/?page_size=100&page=2"
		writeJSON(t, w, dockerHubPage{Count: len(page1) + len(page2), Next: next, Results: page1})
	})

	d.server = httptest.NewServer(mux)
	t.Cleanup(d.server.Close)
	return d
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	body, err := json.Marshal(v)
	require.NoError(t, err)
	_, err = w.Write(body)
	require.NoError(t, err)
}

// withDockerHubBaseURL points the cataloger at the given base URL for the
// duration of the test.
func withDockerHubBaseURL(t *testing.T, baseURL string) {
	t.Helper()
	previous := dockerHubBaseURL
	dockerHubBaseURL = baseURL
	t.Cleanup(func() { dockerHubBaseURL = previous })
}

func TestDockerHubFactory_Match(t *testing.T) {
	f := dockerHubFactory{}
	for _, host := range []string{"docker.io", "index.docker.io", "registry-1.docker.io", "DOCKER.IO"} {
		assert.Truef(t, f.Match(host), "expected match for %q", host)
	}
	for _, host := range []string{"ghcr.io", "quay.io", "example.com"} {
		assert.Falsef(t, f.Match(host), "expected no match for %q", host)
	}
}

func TestDockerHubCataloger_RepositoriesAssembly(t *testing.T) {
	page1 := []dockerHubRepository{
		{Namespace: "myorg", Name: "component-descriptors/componentB"},
		{Namespace: "myorg", Name: "not-a-component"},
	}
	page2 := []dockerHubRepository{
		{Namespace: "", Name: "component-descriptors/componentC"},
	}
	srv := newDockerHubTestServer(t, "myorg", page1, page2)
	withDockerHubBaseURL(t, srv.server.URL)

	cataloger, err := dockerHubFactory{}.New(CatalogerOptions{Host: "docker.io", SubPath: "myorg"})
	require.NoError(t, err)

	var repos []string
	err = cataloger.Repositories(t.Context(), "", func(page []string) error {
		repos = append(repos, page...)
		return nil
	})
	require.NoError(t, err)

	// Raw repository names are assembled as <namespace>/<name>. The second
	// page carries an empty namespace, so the cataloger falls back to the
	// configured namespace. Pagination follows next across both pages.
	assert.Equal(t, []string{
		"myorg/component-descriptors/componentB",
		"myorg/not-a-component",
		"myorg/component-descriptors/componentC",
	}, repos)
}

func TestDockerHubCataloger_ListComponentsThroughLister(t *testing.T) {
	page1 := []dockerHubRepository{
		{Namespace: "myorg", Name: "component-descriptors/componentB"},
		{Namespace: "myorg", Name: "component-descriptors/componentA"},
		{Namespace: "myorg", Name: "not-a-component"},
	}
	page2 := []dockerHubRepository{
		{Namespace: "myorg", Name: "component-descriptors/componentC"},
	}
	srv := newDockerHubTestServer(t, "myorg", page1, page2)
	withDockerHubBaseURL(t, srv.server.URL)

	cataloger, err := dockerHubFactory{}.New(CatalogerOptions{Host: "docker.io", SubPath: "myorg"})
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

func TestDockerHubCataloger_AuthenticatesWithJWT(t *testing.T) {
	page1 := []dockerHubRepository{{Namespace: "myorg", Name: "component-descriptors/componentA"}}
	srv := newDockerHubTestServer(t, "myorg", page1, nil)
	withDockerHubBaseURL(t, srv.server.URL)

	cataloger, err := dockerHubFactory{}.New(CatalogerOptions{
		Host:    "docker.io",
		SubPath: "myorg",
		Credentials: &credsv1.OCICredentials{
			Username: "user",
			Password: "secret",
		},
	})
	require.NoError(t, err)

	err = cataloger.Repositories(t.Context(), "", func(page []string) error { return nil })
	require.NoError(t, err)

	srv.mu.Lock()
	defer srv.mu.Unlock()
	assert.Equal(t, 1, srv.loginCalls, "login endpoint should be called once")
	require.NotEmpty(t, srv.authHeaders)
	for _, h := range srv.authHeaders {
		assert.Equal(t, "JWT "+srv.token, h)
	}
}

func TestDockerHubCataloger_NoAuthWithoutCredentials(t *testing.T) {
	page1 := []dockerHubRepository{{Namespace: "myorg", Name: "component-descriptors/componentA"}}
	srv := newDockerHubTestServer(t, "myorg", page1, nil)
	withDockerHubBaseURL(t, srv.server.URL)

	cataloger, err := dockerHubFactory{}.New(CatalogerOptions{Host: "docker.io", SubPath: "myorg"})
	require.NoError(t, err)

	err = cataloger.Repositories(t.Context(), "", func(page []string) error { return nil })
	require.NoError(t, err)

	srv.mu.Lock()
	defer srv.mu.Unlock()
	assert.Equal(t, 0, srv.loginCalls, "login endpoint should not be called without credentials")
	require.NotEmpty(t, srv.authHeaders)
	for _, h := range srv.authHeaders {
		assert.Empty(t, h, "no Authorization header should be sent without credentials")
	}
}

func TestDockerHubFactory_NewRejectsEmptyNamespace(t *testing.T) {
	_, err := dockerHubFactory{}.New(CatalogerOptions{Host: "docker.io", SubPath: ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace")
}

func TestDockerHubNamespace(t *testing.T) {
	assert.Equal(t, "myorg", dockerHubNamespace("myorg"))
	assert.Equal(t, "myorg", dockerHubNamespace("/myorg/"))
	assert.Equal(t, "myorg", dockerHubNamespace("myorg/sub/path"))
	assert.Equal(t, "", dockerHubNamespace(""))
	assert.Equal(t, "", dockerHubNamespace("/"))
}
