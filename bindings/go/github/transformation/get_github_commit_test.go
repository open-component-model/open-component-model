package transformation

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/credentials"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/github/repository/resource"
	"ocm.software/open-component-model/bindings/go/github/spec/access"
	accessv1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/github/spec/credentials/v1"
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
		ResourceRepository: resource.NewResourceRepository(),
	}

	outDir := t.TempDir()
	step := &v1alpha1.GetGitHubCommit{
		Type: v1alpha1.GetGitHubCommitV1alpha1,
		ID:   "get-github-commit",
		Spec: &v1alpha1.GetGitHubCommitSpec{
			Resource:   githubV2Resource(t, baseURL+"/octocat/Hello-World", testCommit),
			OutputPath: outDir,
		},
	}

	result, err := transformer.Transform(t.Context(), step)
	require.NoError(t, err)

	var transformed v1alpha1.GetGitHubCommit
	require.NoError(t, v1alpha1.Scheme.Convert(result, &transformed))
	require.NotNil(t, transformed.Output)

	require.NotEmpty(t, transformed.Output.File.URI)
	contentPath := strings.TrimPrefix(transformed.Output.File.URI, "file://")
	t.Cleanup(func() { _ = os.Remove(contentPath) })
	assert.Equal(t, outDir, filepath.Dir(contentPath), "the archive must land in the spec's outputPath")

	written, err := os.ReadFile(contentPath)
	require.NoError(t, err)
	assert.Equal(t, payload, written, "buffered file must be the exact archive GitHub served")

	// The output resource is what the Add node's digest propagation reads, so
	// the full spec resource must survive the scheme round-trip intact.
	assert.Equal(t, step.Spec.Resource, transformed.Output.Resource)
}

// TestGetGitHubCommit_Transform_RemovesOutputOnFailure pins that a failing
// download does not leave the buffered output file behind: the graph's cleanup
// node never sees a file that did not reach a consuming node's spec.
func TestGetGitHubCommit_Transform_RemovesOutputOnFailure(t *testing.T) {
	baseURL, _ := mockGitHub(t)
	outDir := t.TempDir()

	transformer := &GetGitHubCommit{
		Scheme:             v1alpha1.Scheme,
		ResourceRepository: resource.NewResourceRepository(),
	}

	// The mock only serves testCommit, so any other commit 404s the archive link.
	_, err := transformer.Transform(t.Context(), &v1alpha1.GetGitHubCommit{
		Type: v1alpha1.GetGitHubCommitV1alpha1,
		ID:   "get-github-commit",
		Spec: &v1alpha1.GetGitHubCommitSpec{
			Resource:   githubV2Resource(t, baseURL+"/octocat/Hello-World", strings.Repeat("a", 40)),
			OutputPath: outDir,
		},
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "error downloading github commit")

	entries, err := os.ReadDir(outDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "a failed download must not leave the output file behind")
}

// stubResolver is a credentials.Resolver test double.
type stubResolver struct {
	typed runtime.Typed
	err   error
	calls int
}

func (s *stubResolver) Resolve(_ context.Context, _ runtime.Identity) (runtime.Typed, error) {
	s.calls++
	return s.typed, s.err
}

// TestGetGitHubCommit_Transform_ResolveCredentials pins the credential policy:
// ErrNotFound downgrades to an anonymous download, any other resolver error
// fails the transform, and resolved credentials reach the download as a token.
func TestGetGitHubCommit_Transform_ResolveCredentials(t *testing.T) {
	step := func(baseURL string) *v1alpha1.GetGitHubCommit {
		return &v1alpha1.GetGitHubCommit{
			Type: v1alpha1.GetGitHubCommitV1alpha1,
			ID:   "get-github-commit",
			Spec: &v1alpha1.GetGitHubCommitSpec{
				Resource: githubV2Resource(t, baseURL+"/octocat/Hello-World", testCommit),
			},
		}
	}

	t.Run("ErrNotFound means anonymous", func(t *testing.T) {
		baseURL, _ := mockGitHub(t)
		resolver := &stubResolver{err: credentials.ErrNotFound}
		transformer := &GetGitHubCommit{
			Scheme:             v1alpha1.Scheme,
			ResourceRepository: resource.NewResourceRepository(),
			CredentialProvider: resolver,
		}

		result, err := transformer.Transform(t.Context(), step(baseURL))
		require.NoError(t, err, "missing credentials must downgrade to an anonymous download")
		require.Equal(t, 1, resolver.calls)

		var transformed v1alpha1.GetGitHubCommit
		require.NoError(t, v1alpha1.Scheme.Convert(result, &transformed))
		t.Cleanup(func() { _ = os.Remove(strings.TrimPrefix(transformed.Output.File.URI, "file://")) })
	})

	t.Run("other resolver errors fail the transform", func(t *testing.T) {
		baseURL, _ := mockGitHub(t)
		transformer := &GetGitHubCommit{
			Scheme:             v1alpha1.Scheme,
			ResourceRepository: resource.NewResourceRepository(),
			CredentialProvider: &stubResolver{err: assert.AnError},
		}

		_, err := transformer.Transform(t.Context(), step(baseURL))
		require.ErrorIs(t, err, assert.AnError)
	})

	t.Run("resolved credentials reach the download", func(t *testing.T) {
		var gotAuth string
		payload := gzippedTar(t, "octocat-Hello-World-"+testCommit+"/README", "hello world")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/repos/octocat/Hello-World/tarball/"+testCommit):
				gotAuth = r.Header.Get("Authorization")
				http.Redirect(w, r, "http://"+r.Host+"/codeload", http.StatusFound)
			case r.URL.Path == "/codeload":
				_, _ = w.Write(payload)
			default:
				http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
			}
		}))
		t.Cleanup(server.Close)

		transformer := &GetGitHubCommit{
			Scheme:             v1alpha1.Scheme,
			ResourceRepository: resource.NewResourceRepository(),
			CredentialProvider: &stubResolver{typed: &credsv1.GitHubCredentials{
				Type:  runtime.NewVersionedType(credsv1.GitHubCredentialsType, credsv1.Version),
				Token: "secret-token",
			}},
		}

		result, err := transformer.Transform(t.Context(), step(server.URL))
		require.NoError(t, err)
		assert.Equal(t, "Bearer secret-token", gotAuth, "the resolved token must authenticate the archive request")

		var transformed v1alpha1.GetGitHubCommit
		require.NoError(t, v1alpha1.Scheme.Convert(result, &transformed))
		t.Cleanup(func() { _ = os.Remove(strings.TrimPrefix(transformed.Output.File.URI, "file://")) })
	})
}

func TestGetGitHubCommit_Transform_RequiresSpec(t *testing.T) { //nolint:dupl
	transformer := &GetGitHubCommit{
		Scheme:             v1alpha1.Scheme,
		ResourceRepository: resource.NewResourceRepository(),
	}

	_, err := transformer.Transform(t.Context(), &v1alpha1.GetGitHubCommit{
		Type: v1alpha1.GetGitHubCommitV1alpha1,
	})
	assert.ErrorContains(t, err, "spec is required for get github commit transformation")
}
