package resource

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	blobpkg "ocm.software/open-component-model/bindings/go/blob"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
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
// returns its base URL and the exact archive bytes it serves. A resource whose
// RepoURL points at "<server>/octocat/Hello-World" drives the real download
// path (enterprise client -> /api/v3/... -> 302 -> archive) against it.
func mockGitHub(t *testing.T) (baseURL string, payload []byte) {
	t.Helper()
	payload = gzippedTar(t, "octocat-Hello-World-"+testCommit+"/README", "hello world")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/repos/octocat/Hello-World/commits/main"):
			_, _ = w.Write([]byte(testCommit))
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

func githubResource(repoURL, commit string) *descriptor.Resource {
	return &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "source", Version: "1.0.0"},
		},
		Access: &v1.GitHub{
			Type:    runtime.NewVersionedType(v1.LegacyType, v1.Version),
			RepoURL: repoURL,
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

func TestResourceRepository_DownloadResource(t *testing.T) {
	t.Run("downloads the commit archive verbatim as a tgz blob", func(t *testing.T) {
		baseURL, payload := mockGitHub(t)
		downloaded, err := NewResourceRepository(nil).DownloadResource(
			t.Context(), githubResource(baseURL+"/octocat/Hello-World", testCommit), nil)
		require.NoError(t, err)

		assert.Equal(t, payload, readBlob(t, downloaded), "blob must be the exact archive GitHub served")

		mt, ok := downloaded.(blobpkg.MediaTypeAware).MediaType()
		require.True(t, ok)
		assert.Equal(t, "application/x-tgz", mt)
	})

	t.Run("rejects a resource with an invalid access spec", func(t *testing.T) {
		_, err := NewResourceRepository(nil).DownloadResource(
			t.Context(), githubResource("https://github.com/octocat/Hello-World", "not-a-sha"), nil)
		assert.ErrorContains(t, err, "commit")
	})

	t.Run("rejects a resource without access", func(t *testing.T) {
		_, err := NewResourceRepository(nil).DownloadResource(t.Context(), &descriptor.Resource{}, nil)
		assert.ErrorContains(t, err, "access")
	})

	t.Run("resolves a ref-only resource to the commit its ref points at", func(t *testing.T) {
		baseURL, payload := mockGitHub(t)
		res := &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: "source", Version: "1.0.0"},
			},
			Access: &v1.GitHub{
				Type:    runtime.NewVersionedType(v1.LegacyType, v1.Version),
				RepoURL: baseURL + "/octocat/Hello-World",
				Ref:     "main",
			},
		}
		downloaded, err := NewResourceRepository(nil).DownloadResource(t.Context(), res, nil)
		require.NoError(t, err)
		assert.Equal(t, payload, readBlob(t, downloaded), "the archive of the resolved commit must be served")
	})

	t.Run("warns when the ref no longer points at the pinned commit and downloads the commit", func(t *testing.T) {
		var logBuf bytes.Buffer
		previous := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
		t.Cleanup(func() { slog.SetDefault(previous) })

		payload := gzippedTar(t, "octocat-Hello-World-"+testCommit+"/README", "hello world")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/repos/octocat/Hello-World/commits/main"):
				// The ref moved on past the pinned commit.
				_, _ = w.Write([]byte("1111111111111111111111111111111111111111"))
			case strings.HasSuffix(r.URL.Path, "/repos/octocat/Hello-World/tarball/"+testCommit):
				http.Redirect(w, r, "http://"+r.Host+"/codeload", http.StatusFound)
			case r.URL.Path == "/codeload":
				_, _ = w.Write(payload)
			default:
				http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
			}
		}))
		t.Cleanup(server.Close)

		res := &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: "source", Version: "1.0.0"},
			},
			Access: &v1.GitHub{
				Type:    runtime.NewVersionedType(v1.LegacyType, v1.Version),
				RepoURL: server.URL + "/octocat/Hello-World",
				Ref:     "main",
				Commit:  testCommit,
			},
		}
		downloaded, err := NewResourceRepository(nil).DownloadResource(t.Context(), res, nil)
		require.NoError(t, err, "ref drift must never fail the download")
		assert.Equal(t, payload, readBlob(t, downloaded), "the pinned commit's archive must be served, not the ref's")
		assert.Contains(t, logBuf.String(), "no longer points at the pinned commit", "the drift must be logged as a warning")
	})

	t.Run("rejects a resource with neither commit nor ref", func(t *testing.T) {
		res := &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: "source", Version: "1.0.0"},
			},
			Access: &v1.GitHub{
				Type:    runtime.NewVersionedType(v1.LegacyType, v1.Version),
				RepoURL: "https://github.com/octocat/Hello-World",
			},
		}
		_, err := NewResourceRepository(nil).DownloadResource(t.Context(), res, nil)
		assert.ErrorContains(t, err, "either commit or ref")
	})
}

// A repository archive can be large, so it is buffered on disk under the
// configured TempFolder rather than held in memory.
func TestResourceRepository_DownloadResource_BuffersToConfiguredTempFolder(t *testing.T) {
	baseURL, payload := mockGitHub(t)
	tempFolder := t.TempDir()

	repo := NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: tempFolder})
	downloaded, err := repo.DownloadResource(t.Context(), githubResource(baseURL+"/octocat/Hello-World", testCommit), nil)
	require.NoError(t, err)

	entries, err := os.ReadDir(tempFolder)
	require.NoError(t, err)
	require.Len(t, entries, 1, "the archive must be buffered under the configured temp folder")

	assert.Equal(t, payload, readBlob(t, downloaded), "the buffered blob must read back the exact archive")

	closer, ok := downloaded.(io.Closer)
	require.True(t, ok, "the returned blob must be closeable so its file can be reclaimed")
	require.NoError(t, closer.Close())

	entries, err = os.ReadDir(tempFolder)
	require.NoError(t, err)
	assert.Empty(t, entries, "closing the blob must remove the buffered file")
}

func TestResourceRepository_GetResourceCredentialConsumerIdentity(t *testing.T) {
	identity, err := NewResourceRepository(nil).GetResourceCredentialConsumerIdentity(t.Context(),
		githubResource("https://github.com/open-component-model/ocm", testCommit))
	require.NoError(t, err)

	assert.Equal(t, "GitHubRepository", identity[runtime.IdentityAttributeType])
	assert.Equal(t, "github.com", identity[runtime.IdentityAttributeHostname])
}

func TestResourceRepository_UploadResource(t *testing.T) {
	_, err := NewResourceRepository(nil).UploadResource(t.Context(), nil, nil, nil)
	assert.ErrorContains(t, err, "not support")
}

func TestResourceRepository_GetResourceRepositoryScheme(t *testing.T) {
	scheme := NewResourceRepository(nil).GetResourceRepositoryScheme()
	require.NotNil(t, scheme)
	assert.True(t, scheme.IsRegistered(runtime.NewVersionedType(v1.LegacyType, v1.Version)))
}
