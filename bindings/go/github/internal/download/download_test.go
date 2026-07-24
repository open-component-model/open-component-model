package download

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	godigest "github.com/opencontainers/go-digest"
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

func testAccess(serverURL string) *v1.GitHub {
	return &v1.GitHub{
		RepoURL: serverURL + "/octocat/Hello-World",
		Commit:  downloadTestCommit,
	}
}

func TestDownload(t *testing.T) {
	t.Run("streams the archive as an application/x-tgz blob", func(t *testing.T) {
		payload := gzippedTar(t, "octocat-Hello-World-"+downloadTestCommit+"/README", "hello world")
		server := archiveServer(t, payload)

		downloaded, err := Download(t.Context(), testAccess(server.URL), nil, nil, "")
		require.NoError(t, err)

		reader, err := downloaded.ReadCloser()
		require.NoError(t, err)
		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.NoError(t, reader.Close())
		assert.Equal(t, payload, data, "blob must be the exact archive GitHub served")

		mt, ok := downloaded.(blob.MediaTypeAware).MediaType()
		require.True(t, ok)
		assert.Equal(t, MediaTypeTGZ, mt)

		assert.Equal(t, blob.SizeUnknown, downloaded.(blob.SizeAware).Size(), "a stream has no known size")
	})

	t.Run("computes the digest on the fly while streaming", func(t *testing.T) {
		payload := gzippedTar(t, "octocat-Hello-World-"+downloadTestCommit+"/README", "hello world")
		server := archiveServer(t, payload)

		downloaded, err := Download(t.Context(), testAccess(server.URL), nil, nil, "")
		require.NoError(t, err)

		_, known := downloaded.(blob.DigestAware).Digest()
		assert.False(t, known, "the digest cannot be known before the stream is read")

		reader, err := downloaded.ReadCloser()
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, reader)
		require.NoError(t, err)
		require.NoError(t, reader.Close())

		computed, known := downloaded.(blob.DigestAware).Digest()
		require.True(t, known, "the digest must be known once the stream is fully read")
		assert.Equal(t, godigest.FromBytes(payload).String(), computed)
	})

	t.Run("serves the stream exactly once", func(t *testing.T) {
		payload := gzippedTar(t, "octocat-Hello-World-"+downloadTestCommit+"/README", "hello world")
		server := archiveServer(t, payload)

		downloaded, err := Download(t.Context(), testAccess(server.URL), nil, nil, "")
		require.NoError(t, err)

		reader, err := downloaded.ReadCloser()
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, reader)
		require.NoError(t, err)
		require.NoError(t, reader.Close())

		_, err = downloaded.ReadCloser()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already consumed")
	})

	t.Run("verifies a matching expected digest on close", func(t *testing.T) {
		payload := gzippedTar(t, "octocat-Hello-World-"+downloadTestCommit+"/README", "hello world")
		server := archiveServer(t, payload)

		downloaded, err := Download(t.Context(), testAccess(server.URL), nil, nil, godigest.FromBytes(payload))
		require.NoError(t, err)

		reader, err := downloaded.ReadCloser()
		require.NoError(t, err)
		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		assert.Equal(t, payload, data)
		require.NoError(t, reader.Close(), "a matching digest must verify on close")
	})

	t.Run("rejects a mismatched expected digest on close", func(t *testing.T) {
		payload := gzippedTar(t, "octocat-Hello-World-"+downloadTestCommit+"/README", "hello world")
		server := archiveServer(t, payload)

		downloaded, err := Download(t.Context(), testAccess(server.URL), nil, nil, godigest.FromString("something else"))
		require.NoError(t, err)

		reader, err := downloaded.ReadCloser()
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, reader)
		require.NoError(t, err)
		err = reader.Close()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "digest mismatch")
	})

	t.Run("rejects closing a partially read stream when a digest is expected", func(t *testing.T) {
		payload := gzippedTar(t, "octocat-Hello-World-"+downloadTestCommit+"/README", "hello world")
		server := archiveServer(t, payload)

		downloaded, err := Download(t.Context(), testAccess(server.URL), nil, nil, godigest.FromBytes(payload))
		require.NoError(t, err)

		reader, err := downloaded.ReadCloser()
		require.NoError(t, err)
		// Read a single byte, then close: a digest of a prefix proves nothing.
		_, err = reader.Read(make([]byte, 1))
		require.NoError(t, err)
		err = reader.Close()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "before being fully read")
	})

	t.Run("allows abandoning a partially read stream when no digest is expected", func(t *testing.T) {
		// A calculate-only stream holds no expectation; a consumer that stops
		// reading must be able to close without a false alarm.
		payload := gzippedTar(t, "octocat-Hello-World-"+downloadTestCommit+"/README", "hello world")
		server := archiveServer(t, payload)

		downloaded, err := Download(t.Context(), testAccess(server.URL), nil, nil, "")
		require.NoError(t, err)

		reader, err := downloaded.ReadCloser()
		require.NoError(t, err)
		_, err = reader.Read(make([]byte, 1))
		require.NoError(t, err)
		require.NoError(t, reader.Close(), "closing an abandoned calculate-only stream must not fail")

		_, known := downloaded.(blob.DigestAware).Digest()
		assert.False(t, known, "an abandoned stream has no digest to report")
	})

	t.Run("rejects a malformed expected digest before downloading", func(t *testing.T) {
		_, err := Download(t.Context(), &v1.GitHub{
			RepoURL: "https://github.com/octocat/Hello-World",
			Commit:  downloadTestCommit,
		}, nil, nil, godigest.Digest("sha256:not-a-digest"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid expected digest")
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
