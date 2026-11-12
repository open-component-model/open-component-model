package githubtest_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/internal/testutil/githubtest"
)

func get(t *testing.T, client *http.Client, url, auth string) (*http.Response, []byte) {
	t.Helper()
	r := require.New(t)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	r.NoError(err)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := client.Do(req)
	r.NoError(err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	r.NoError(err)
	return resp, body
}

func TestStart(t *testing.T) {
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	repoAPI := "/api/v3/repos/" + githubtest.Owner + "/" + githubtest.Repo

	t.Run("resolves the ref to the default commit", func(t *testing.T) {
		r := require.New(t)
		srv := githubtest.Start(t)
		r.Equal(srv.URL+"/octocat/Hello-World", srv.RepoURL)

		resp, body := get(t, http.DefaultClient, srv.URL+repoAPI+"/commits/"+githubtest.Ref, "")
		r.Equal(http.StatusOK, resp.StatusCode)
		r.Equal(githubtest.Commit, string(body))
		r.Equal(1, srv.ResolveCalls())
	})

	t.Run("resolves the ref to the commit WithCommit sets", func(t *testing.T) {
		r := require.New(t)
		moved := strings.Repeat("1", 40)
		srv := githubtest.Start(t, githubtest.WithCommit(moved))

		_, body := get(t, http.DefaultClient, srv.URL+repoAPI+"/commits/"+githubtest.Ref, "")
		r.Equal(moved, string(body))
	})

	t.Run("redirects the tarball to codeload and records the authorization", func(t *testing.T) {
		r := require.New(t)
		srv := githubtest.Start(t)

		resp, _ := get(t, noRedirect, srv.URL+repoAPI+"/tarball/"+githubtest.Commit, "Bearer token")
		r.Equal(http.StatusFound, resp.StatusCode)
		r.Equal(srv.URL+"/codeload", resp.Header.Get("Location"))
		r.Equal("Bearer token", srv.Authorization())
	})

	t.Run("serves the default payload as a gzipped tar", func(t *testing.T) {
		r := require.New(t)
		srv := githubtest.Start(t)

		resp, body := get(t, http.DefaultClient, srv.URL+repoAPI+"/tarball/"+githubtest.Commit, "")
		r.Equal(http.StatusOK, resp.StatusCode)
		r.Equal(srv.Payload, body)

		gz, err := gzip.NewReader(bytes.NewReader(body))
		r.NoError(err)
		hdr, err := tar.NewReader(gz).Next()
		r.NoError(err)
		r.Equal("octocat-Hello-World-"+githubtest.Commit+"/README", hdr.Name)
	})

	t.Run("serves the payload WithPayload sets", func(t *testing.T) {
		r := require.New(t)
		srv := githubtest.Start(t, githubtest.WithPayload([]byte("abc")))

		_, body := get(t, http.DefaultClient, srv.URL+"/codeload", "")
		r.Equal("abc", string(body))
	})

	t.Run("rejects other paths", func(t *testing.T) {
		r := require.New(t)
		srv := githubtest.Start(t)

		resp, _ := get(t, http.DefaultClient, srv.URL+repoAPI+"/tarball/"+strings.Repeat("a", 40), "")
		r.Equal(http.StatusNotFound, resp.StatusCode)
		r.Zero(srv.ResolveCalls())
	})
}
