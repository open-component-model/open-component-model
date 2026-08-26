package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	t.Run("returns the archive as an application/x-tgz blob", func(t *testing.T) {
		payload := gzippedTar(t, "octocat-Hello-World-"+downloadTestCommit+"/README", "hello world")
		server := archiveServer(t, payload)

		downloaded, err := Download(t.Context(), &v1.GitHub{
			RepoURL: server.URL + "/octocat/Hello-World",
			Commit:  downloadTestCommit,
		}, nil, nil)
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
		}, nil, nil)
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
		}, nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "error resolving github archive link")
	})
}

func TestRedact(t *testing.T) {
	t.Run("strips the token of a url rendered by net/http", func(t *testing.T) {
		err := &url.Error{
			Op:  "Get",
			URL: "https://codeload.github.com/octocat/Hello-World/legacy.tar.gz/7fd1a60?token=supersecret",
			Err: context.Canceled,
		}
		got := redact(err)
		assert.Equal(t, `Get "https://codeload.github.com/octocat/Hello-World/legacy.tar.gz/7fd1a60?token=REDACTED": context canceled`, got.Error())
	})

	t.Run("keeps every other parameter legible", func(t *testing.T) {
		err := &url.Error{
			Op:  "Get",
			URL: "https://codeload.github.com/o/r?ref=main&token=supersecret&format=tarball",
			Err: context.Canceled,
		}
		got := redact(err).Error()
		assert.Contains(t, got, "ref=main")
		assert.Contains(t, got, "format=tarball", "redaction must stop at the parameter separator")
		assert.NotContains(t, got, "supersecret")
	})

	t.Run("strips a token quoted inside a response header", func(t *testing.T) {
		// The shape go-github produces when net/http cannot parse the Location
		// header it reads the archive link from.
		err := errors.New(`malformed MIME header line: "Location: https://codeload.github.com/o/r?token=supersecret\x7f"`)
		assert.NotContains(t, redact(err).Error(), "supersecret")
	})

	t.Run("keeps the cause matchable", func(t *testing.T) {
		err := &url.Error{Op: "Get", URL: "https://example.com/a?token=secret", Err: context.Canceled}
		require.ErrorIs(t, redact(err), context.Canceled)

		var urlErr *url.Error
		require.ErrorAs(t, redact(err), &urlErr, "the chain must stay walkable for callers that classify by type")
	})

	t.Run("survives further wrapping", func(t *testing.T) {
		err := &url.Error{Op: "Get", URL: "https://example.com/a?token=secret", Err: context.Canceled}
		wrapped := fmt.Errorf("error downloading github archive o/r@sha: %w", redact(err))
		assert.NotContains(t, wrapped.Error(), "secret")
		assert.ErrorIs(t, wrapped, context.Canceled)
	})

	t.Run("returns an error with no token unchanged", func(t *testing.T) {
		// The common go-github failure: an API error naming an unsigned URL.
		err := errors.New("GET https://api.github.com/repos/octocat/Hello-World/tarball/7fd1a60: 404 Not Found []")
		assert.Same(t, err, redact(err), "an error with nothing to redact must not be wrapped needlessly")
	})

	t.Run("leaves a public archive link alone", func(t *testing.T) {
		// A public repository redirects to codeload without a token at all.
		err := &url.Error{Op: "Get", URL: "https://codeload.github.com/octocat/Hello-World/legacy.tar.gz/7fd1a60", Err: context.Canceled}
		assert.Equal(t, err.Error(), redact(err).Error())
	})
}
