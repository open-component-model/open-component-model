package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	credv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func directCredentials(props map[string]string) *credv1.DirectCredentials {
	return &credv1.DirectCredentials{
		Type:       runtime.NewVersionedType(credv1.CredentialsType, credv1.Version),
		Properties: props,
	}
}

func TestTokenFromCredentials(t *testing.T) {
	t.Run("nil credentials mean anonymous access", func(t *testing.T) {
		token, err := TokenFromCredentials(nil)
		require.NoError(t, err)
		assert.Empty(t, token)
	})

	t.Run("token property is used", func(t *testing.T) {
		token, err := TokenFromCredentials(directCredentials(map[string]string{"token": "ghp_secret"}))
		require.NoError(t, err)
		assert.Equal(t, "ghp_secret", token)
	})

	t.Run("accessToken is accepted as an alias", func(t *testing.T) {
		token, err := TokenFromCredentials(directCredentials(map[string]string{"accessToken": "ghp_secret"}))
		require.NoError(t, err)
		assert.Equal(t, "ghp_secret", token)
	})

	t.Run("present but unusable credentials are rejected", func(t *testing.T) {
		_, err := TokenFromCredentials(directCredentials(map[string]string{"username": "octocat"}))
		assert.ErrorContains(t, err, "token")
	})
}

func TestParseOwnerRepo(t *testing.T) {
	t.Run("parses host/owner/repo", func(t *testing.T) {
		owner, repo, err := ParseOwnerRepo("https://github.com/octocat/Hello-World")
		require.NoError(t, err)
		assert.Equal(t, "octocat", owner)
		assert.Equal(t, "Hello-World", repo)
	})

	t.Run("trims a .git suffix", func(t *testing.T) {
		_, repo, err := ParseOwnerRepo("https://github.com/octocat/Hello-World.git")
		require.NoError(t, err)
		assert.Equal(t, "Hello-World", repo)
	})

	t.Run("rejects a URL without owner/repo", func(t *testing.T) {
		_, _, err := ParseOwnerRepo("https://github.com/octocat")
		assert.Error(t, err)
	})
}

// gzippedTar builds a gzipped tar archive containing a single file.
func gzippedTar(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}))
	_, err := tw.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func TestResolveCommit(t *testing.T) {
	const ref = "main"
	const wantSHA = "7fd1a60b01f91b314f59955a4e4d4e80d8edf11d"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// go-github calls GET repos/{owner}/{repo}/commits/{ref} for GetCommitSHA1.
		// A non-github.com host takes the enterprise path (/api/v3/...), so match by suffix.
		if strings.HasSuffix(r.URL.Path, "/repos/octocat/Hello-World/commits/"+ref) {
			_, _ = w.Write([]byte(wantSHA))
			return
		}
		http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
	}))
	defer server.Close()

	got, err := ResolveCommit(t.Context(), server.URL+"/octocat/Hello-World", "", ref, "")
	require.NoError(t, err)
	assert.Equal(t, wantSHA, got)
}

func TestFetchCommitArchive(t *testing.T) {
	const commit = "7fd1a60b01f91b314f59955a4e4d4e80d8edf11d"
	payload := gzippedTar(t, "Hello-World-"+commit+"/README", "hello")

	var authSeen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/repos/octocat/Hello-World/tarball/"+commit):
			authSeen = r.Header.Get("Authorization")
			http.Redirect(w, r, "http://"+r.Host+"/codeload/octocat/Hello-World/"+commit, http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/codeload/"):
			_, _ = w.Write(payload)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer server.Close()

	gh, err := NewGitHubClient(server.URL+"/octocat/Hello-World", "", "ghp_secret", server.Client())
	require.NoError(t, err)

	data, err := FetchCommitArchive(t.Context(), gh, server.Client(), "octocat", "Hello-World", commit)
	require.NoError(t, err)

	assert.Equal(t, payload, data, "downloaded archive must be the exact bytes GitHub served")
	assert.Equal(t, "Bearer ghp_secret", authSeen, "the API request must carry the bearer token")

	// sanity: the payload is a valid gzipped tar with the README entry
	gz, err := gzip.NewReader(bytes.NewReader(data))
	require.NoError(t, err)
	tr := tar.NewReader(gz)
	h, err := tr.Next()
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(h.Name, "/README"))
	body, err := io.ReadAll(tr)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(body))
}
