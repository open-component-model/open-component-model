package download

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/github/spec/credentials/v1"
)

func TestOwnerRepo(t *testing.T) {
	// ownerRepo takes an already-parsed URL, so parse through parseRepoURL to
	// exercise the same path clientFor takes.
	split := func(t *testing.T, repoURL string) (string, string, error) {
		t.Helper()
		u, err := parseRepoURL(repoURL)
		require.NoError(t, err)
		return ownerRepo(u, repoURL)
	}

	t.Run("parses host/owner/repo", func(t *testing.T) {
		owner, repo, err := split(t, "https://github.com/octocat/Hello-World")
		require.NoError(t, err)
		assert.Equal(t, "octocat", owner)
		assert.Equal(t, "Hello-World", repo)
	})

	t.Run("trims a .git suffix", func(t *testing.T) {
		_, repo, err := split(t, "https://github.com/octocat/Hello-World.git")
		require.NoError(t, err)
		assert.Equal(t, "Hello-World", repo)
	})

	t.Run("rejects a URL without owner/repo", func(t *testing.T) {
		_, _, err := split(t, "https://github.com/octocat")
		assert.Error(t, err)
	})

	t.Run("quotes the url as supplied, not the scheme-normalized form", func(t *testing.T) {
		_, _, err := split(t, "github.com/octocat")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `"github.com/octocat"`, "the error must quote the caller's input")
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

	got, err := ResolveCommit(t.Context(), &v1.GitHub{RepoURL: server.URL + "/octocat/Hello-World", Ref: ref}, nil, server.Client())
	require.NoError(t, err)
	assert.Equal(t, wantSHA, got)
}

func TestResolveCommit_APIHostnameOverridesTheRepoHost(t *testing.T) {
	const ref = "main"
	const wantSHA = "7fd1a60b01f91b314f59955a4e4d4e80d8edf11d"

	var pathSeen string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/repos/octocat/Hello-World/commits/"+ref) {
			pathSeen = r.URL.Path
			_, _ = w.Write([]byte(wantSHA))
			return
		}
		http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
	}))
	defer api.Close()

	// The repository host is unreachable, so resolution can only succeed when
	// the apiHostname override is what the client actually talks to.
	apiURL, err := url.Parse(api.URL)
	require.NoError(t, err)
	access := &v1.GitHub{
		RepoURL:     "http://github.invalid/octocat/Hello-World",
		APIHostname: apiURL.Host,
		Ref:         ref,
	}

	got, err := ResolveCommit(t.Context(), access, nil, api.Client())
	require.NoError(t, err)
	assert.Equal(t, wantSHA, got)
	assert.True(t, strings.HasPrefix(pathSeen, "/api/v3/"),
		"the override host must be addressed as GitHub Enterprise, got %q", pathSeen)
}

func TestResolveCommit_UnresolvableRef(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no such ref", http.StatusNotFound)
	}))
	defer server.Close()

	_, err := ResolveCommit(t.Context(), &v1.GitHub{RepoURL: server.URL + "/octocat/Hello-World", Ref: "gone"}, nil, server.Client())
	require.Error(t, err)
	assert.ErrorContains(t, err, `error resolving github ref "gone" for octocat/Hello-World`)
}

func TestFetch(t *testing.T) {
	const commit = "7fd1a60b01f91b314f59955a4e4d4e80d8edf11d"
	payload := gzippedTar(t, "Hello-World-"+commit+"/README", "hello")

	// sanity: the fixture is a real gzipped tar, so comparing the fetched bytes
	// against it below is a meaningful assertion.
	gz, err := gzip.NewReader(bytes.NewReader(payload))
	require.NoError(t, err)
	tr := tar.NewReader(gz)
	h, err := tr.Next()
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(h.Name, "/README"))
	body, err := io.ReadAll(tr)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(body))

	// Absent credentials must send no Authorization header at all, not an empty
	// bearer token, which GitHub rejects outright.
	for _, tc := range []struct {
		name        string
		credentials *credsv1.GitHubCredentials
		wantAuth    []string // nil means the header must be absent entirely
	}{
		{
			name:        "a token authenticates the request",
			credentials: &credsv1.GitHubCredentials{Token: "ghp_secret"},
			wantAuth:    []string{"Bearer ghp_secret"},
		},
		{
			name:        "no credentials leave the request anonymous",
			credentials: nil,
			wantAuth:    nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var authSeen []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/repos/octocat/Hello-World/tarball/"+commit):
					authSeen = r.Header.Values("Authorization")
					http.Redirect(w, r, "http://"+r.Host+"/codeload/octocat/Hello-World/"+commit, http.StatusFound)
				case strings.HasPrefix(r.URL.Path, "/codeload/"):
					_, _ = w.Write(payload)
				default:
					http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
				}
			}))
			defer server.Close()

			// fetch streams the archive so it can be buffered to the filesystem
			// rather than read wholly into memory.
			access := &v1.GitHub{RepoURL: server.URL + "/octocat/Hello-World", Commit: commit}
			rc, err := fetch(t.Context(), access, tc.credentials, server.Client())
			require.NoError(t, err)
			defer func() { require.NoError(t, rc.Close()) }()

			data, err := io.ReadAll(rc)
			require.NoError(t, err)

			assert.Equal(t, payload, data, "downloaded archive must be the exact bytes GitHub served")
			assert.Equal(t, tc.wantAuth, authSeen, "the API request must carry exactly the expected Authorization header")
		})
	}
}

// The pre-signed archive endpoint can fail independently of the API call that
// resolved it, for example when the link has expired.
func TestFetch_ArchiveEndpointStatusError(t *testing.T) {
	const commit = "7fd1a60b01f91b314f59955a4e4d4e80d8edf11d"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/repos/octocat/Hello-World/tarball/"+commit):
			http.Redirect(w, r, "http://"+r.Host+"/codeload/octocat/Hello-World/"+commit, http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/codeload/"):
			http.Error(w, "gone", http.StatusGone)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer server.Close()

	access := &v1.GitHub{RepoURL: server.URL + "/octocat/Hello-World", Commit: commit}
	_, err := fetch(t.Context(), access, nil, server.Client())
	require.Error(t, err)
	assert.ErrorContains(t, err, "unexpected status downloading github archive octocat/Hello-World@"+commit+": 410 Gone")
}

// For a private repository GitHub signs the archive link with a short-lived
// token in its query string. net/http records the full request URL in the
// *url.Error it returns, so wrapping a download failure verbatim would print
// that token into the error and from there into the caller's logs.
func TestFetch_DownloadErrorDoesNotLeakTheSignedArchiveLink(t *testing.T) {
	const commit = "7fd1a60b01f91b314f59955a4e4d4e80d8edf11d"
	const signedToken = "supersecretarchivetoken"

	// A server that is started and immediately closed yields an address nothing
	// listens on, so the archive download fails at dial time — which is what
	// makes net/http wrap the signed link into a *url.Error.
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/repos/octocat/Hello-World/tarball/"+commit) {
			http.Redirect(w, r, deadURL+"/codeload/octocat/Hello-World/"+commit+"?token="+signedToken, http.StatusFound)
			return
		}
		http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
	}))
	defer server.Close()

	access := &v1.GitHub{RepoURL: server.URL + "/octocat/Hello-World", Commit: commit}
	_, err := fetch(t.Context(), access, &credsv1.GitHubCredentials{Token: "ghp_secret"}, server.Client())

	require.Error(t, err)
	assert.NotContains(t, err.Error(), signedToken, "the signed archive link's token must not reach the error")
	assert.NotContains(t, err.Error(), "ghp_secret", "the caller's github token must not reach the error")
	assert.Contains(t, err.Error(), "octocat/Hello-World@"+commit,
		"the commit coordinates must survive so the failure stays diagnosable")
	assert.ErrorContains(t, err, "connection refused", "the underlying cause must survive")
}
