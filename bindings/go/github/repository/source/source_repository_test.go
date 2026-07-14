package source

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	blobpkg "ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const testCommit = "7fd1a60b01f91b314f59955a4e4d4e80d8edf11d"

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

// mockGitHub stands up an httptest server emulating the GitHub archive API and
// returns its base URL and the exact archive bytes it serves.
func mockGitHub(t *testing.T) (baseURL string, payload []byte) {
	t.Helper()
	payload = gzippedTar(t, "octocat-Hello-World-"+testCommit+"/README", "hello world")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/repos/octocat/Hello-World/tarball/"+testCommit):
			http.Redirect(w, r, "http://"+r.Host+"/codeload", http.StatusFound)
		case r.URL.Path == "/codeload":
			_, _ = w.Write(payload)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL, payload
}

func githubSource(repoURL, ref, commit string) *descriptor.Source {
	return &descriptor.Source{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "source", Version: "1.0.0"},
		},
		Access: &v1.GitHub{
			Type:    runtime.NewVersionedType(v1.LegacyType, v1.Version),
			RepoURL: repoURL,
			Ref:     ref,
			Commit:  commit,
		},
	}
}

func readBlob(t *testing.T, b blobpkg.ReadOnlyBlob) []byte {
	t.Helper()
	reader, err := b.ReadCloser()
	require.NoError(t, err)
	defer func() { require.NoError(t, reader.Close()) }()
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	return data
}

func TestSourceRepository_DownloadSource(t *testing.T) {
	t.Run("downloads the commit archive verbatim as a tgz blob", func(t *testing.T) {
		baseURL, payload := mockGitHub(t)
		downloaded, err := NewSourceRepository().DownloadSource(
			t.Context(), githubSource(baseURL+"/octocat/Hello-World", "refs/heads/master", testCommit))
		require.NoError(t, err)

		assert.Equal(t, payload, readBlob(t, downloaded), "blob must be the exact archive GitHub served")

		mt, ok := downloaded.(blobpkg.MediaTypeAware).MediaType()
		require.True(t, ok)
		assert.Equal(t, "application/x-tgz", mt)
	})

	t.Run("rejects a source without access", func(t *testing.T) {
		_, err := NewSourceRepository().DownloadSource(t.Context(), &descriptor.Source{})
		assert.ErrorContains(t, err, "access")
	})

	t.Run("rejects a ref-only source with no pinned commit", func(t *testing.T) {
		_, err := NewSourceRepository().DownloadSource(t.Context(),
			githubSource("https://github.com/octocat/Hello-World", "refs/heads/master", ""))
		assert.ErrorContains(t, err, "requires a pinned commit")
	})
}

// A GitHub 5xx is retryable, so the number of requests the server sees is a
// direct readout of the retry policy the repository's HTTP client was built
// with. Driving two different WithHTTPConfig values to two different request
// counts fails if the option is dropped on the floor.
func TestSourceRepository_WithHTTPConfig_IsAppliedToRequests(t *testing.T) {
	maxRetries := func(n int) *httpv1alpha1.Config {
		return &httpv1alpha1.Config{
			Retry: &httpv1alpha1.RetryConfig{
				MaxRetries: &n,
				MinWait:    httpv1alpha1.NewTimeout(time.Millisecond),
				MaxWait:    httpv1alpha1.NewTimeout(2 * time.Millisecond),
			},
		}
	}

	for _, tc := range []struct {
		name     string
		config   *httpv1alpha1.Config
		attempts int
	}{
		{name: "retry disabled", config: maxRetries(-1), attempts: 1},
		{name: "two retries", config: maxRetries(2), attempts: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests++
				http.Error(w, "boom", http.StatusInternalServerError)
			}))
			t.Cleanup(server.Close)

			repo := NewSourceRepository(WithHTTPConfig(tc.config))
			_, err := repo.DownloadSource(t.Context(),
				githubSource(server.URL+"/octocat/Hello-World", "", testCommit))

			require.Error(t, err, "a 500 from the archive link endpoint must fail the download")
			assert.Equal(t, tc.attempts, requests, "the configured retry policy must govern the request count")
		})
	}
}

// countingTransport counts the requests it carries; a positive count after a
// download proves the injected client, not a config-built one, did the work.
type countingTransport struct {
	requests int
}

func (c *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.requests++
	return http.DefaultTransport.RoundTrip(req)
}

func TestSourceRepository_WithHTTPClient_IsUsedForRequests(t *testing.T) {
	baseURL, payload := mockGitHub(t)
	transport := &countingTransport{}

	repo := NewSourceRepository(WithHTTPClient(&http.Client{Transport: transport}))
	downloaded, err := repo.DownloadSource(t.Context(),
		githubSource(baseURL+"/octocat/Hello-World", "", testCommit))
	require.NoError(t, err)

	assert.Equal(t, payload, readBlob(t, downloaded), "the download must work through the injected client")
	assert.Positive(t, transport.requests, "the injected client must carry the requests")
}

func TestSourceRepository_UploadSource(t *testing.T) {
	_, err := NewSourceRepository().UploadSource(t.Context(), nil, nil, nil)
	assert.ErrorContains(t, err, "not support")
}
