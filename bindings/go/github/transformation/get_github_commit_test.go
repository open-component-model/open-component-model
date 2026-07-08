package transformation

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/github/repository/resource"
	"ocm.software/open-component-model/bindings/go/github/spec/access"
	accessv1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/github/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const testCommit = "7fd1a60b01f91b314f59955a4e4d4e80d8edf11d"

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

// mockGitHub emulates the GitHub archive API and returns its base URL plus the
// exact archive bytes served.
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

func githubV2Resource(t *testing.T, repoURL, commit string) *v2.Resource {
	t.Helper()
	gitHubAccess := &accessv1.GitHub{
		Type:    runtime.NewVersionedType(accessv1.LegacyType, accessv1.Version),
		RepoURL: repoURL,
		Commit:  commit,
	}
	raw := runtime.Raw{}
	require.NoError(t, access.Scheme.Convert(gitHubAccess, &raw))
	return &v2.Resource{
		ElementMeta: v2.ElementMeta{
			ObjectMeta: v2.ObjectMeta{Name: "source", Version: "1.0.0"},
		},
		Access: &raw,
	}
}

func TestGetGitHubCommit_Transform(t *testing.T) {
	baseURL, payload := mockGitHub(t)

	transformer := &GetGitHubCommit{
		Scheme:             v1alpha1.Scheme,
		ResourceRepository: resource.NewResourceRepository(nil),
	}

	step := &v1alpha1.GetGitHubCommit{
		Type: v1alpha1.GetGitHubCommitV1alpha1,
		ID:   "get-github-commit",
		Spec: &v1alpha1.GetGitHubCommitSpec{
			Resource: githubV2Resource(t, baseURL+"/octocat/Hello-World", testCommit),
		},
	}

	result, err := transformer.Transform(t.Context(), step)
	require.NoError(t, err)

	var transformed v1alpha1.GetGitHubCommit
	require.NoError(t, v1alpha1.Scheme.Convert(result, &transformed))
	require.NotNil(t, transformed.Output)

	require.NotEmpty(t, transformed.Output.ContentFile.URI)
	contentPath := strings.TrimPrefix(transformed.Output.ContentFile.URI, "file://")
	t.Cleanup(func() { _ = os.Remove(contentPath) })

	written, err := os.ReadFile(contentPath)
	require.NoError(t, err)
	assert.Equal(t, payload, written, "buffered file must be the exact archive GitHub served")

	require.NotNil(t, transformed.Output.Resource)
	assert.Equal(t, "source", transformed.Output.Resource.Name)
}

func TestGetGitHubCommit_Transform_RequiresSpec(t *testing.T) { //nolint:dupl
	transformer := &GetGitHubCommit{
		Scheme:             v1alpha1.Scheme,
		ResourceRepository: resource.NewResourceRepository(nil),
	}

	_, err := transformer.Transform(t.Context(), &v1alpha1.GetGitHubCommit{
		Type: v1alpha1.GetGitHubCommitV1alpha1,
	})
	assert.ErrorContains(t, err, "spec")
}

