package download

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	v1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
)

const downloadTestCommit = "7fd1a60b01f91b314f59955a4e4d4e80d8edf11d"

// archiveServer emulates the GitHub archive API: the tarball endpoint answers
// with a 302 to a codeload path serving payload; every other path is a 404.
func archiveServer(t *testing.T, payload []byte) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/repos/octocat/Hello-World/tarball/"+downloadTestCommit):
			http.Redirect(w, r, "http://"+r.Host+"/codeload", http.StatusFound)
		case r.URL.Path == "/codeload":
			_, _ = w.Write(payload)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestDownload(t *testing.T) {
	t.Run("returns the archive as a file-backed application/x-tgz blob", func(t *testing.T) {
		payload := gzippedTar(t, "octocat-Hello-World-"+downloadTestCommit+"/README", "hello world")
		server := archiveServer(t, payload)

		downloaded, err := Download(t.Context(), &v1.GitHub{
			RepoURL: server.URL + "/octocat/Hello-World",
			Commit:  downloadTestCommit,
		}, nil, nil, "")
		require.NoError(t, err)

		reader, err := downloaded.ReadCloser()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, reader.Close()) })
		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		assert.Equal(t, payload, data, "blob must be the exact archive GitHub served")

		mt, ok := downloaded.(blob.MediaTypeAware).MediaType()
		require.True(t, ok)
		assert.Equal(t, MediaTypeTGZ, mt)

		size := downloaded.(blob.SizeAware).Size()
		assert.Equal(t, int64(len(payload)), size, "blob must report the archive size")
	})

	t.Run("rejects an access without a pinned commit", func(t *testing.T) {
		_, err := Download(t.Context(), &v1.GitHub{
			RepoURL: "https://github.com/octocat/Hello-World",
			Ref:     "main",
		}, nil, nil, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pinned commit")
	})

	t.Run("fails when the archive link cannot be resolved", func(t *testing.T) {
		// No tarball route: GetArchiveLink gets a 404 (404 is not retried by
		// the default client, unlike 5xx).
		server := archiveServer(t, nil)
		_, err := Download(t.Context(), &v1.GitHub{
			RepoURL: server.URL + "/octocat/No-Such-Repo",
			Commit:  downloadTestCommit,
		}, nil, nil, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "error resolving github archive link")
	})
}
