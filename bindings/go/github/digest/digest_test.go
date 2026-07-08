package digest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
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
// exact archive bytes served, so the digest processor's real download path can
// run against it.
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

func githubResourceRef(repoURL, ref, commit string) *descriptor.Resource {
	return &descriptor.Resource{
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

func TestDigestProcessor_ProcessResourceDigest(t *testing.T) {
	baseURL, payload := mockGitHub(t)
	// The digest matches old OCM: a generic blob digest over the exact gzipped
	// archive bytes GitHub serves.
	expected := godigest.FromBytes(payload)
	repoURL := baseURL + "/octocat/Hello-World"
	processor := NewDigestProcessor(nil)

	t.Run("applies the generic blob digest of the downloaded archive", func(t *testing.T) {
		res := githubResource(repoURL, testCommit)
		processed, err := processor.ProcessResourceDigest(t.Context(), res, nil)
		require.NoError(t, err)

		require.NotNil(t, processed.Digest)
		assert.Equal(t, "SHA-256", processed.Digest.HashAlgorithm)
		assert.Equal(t, "genericBlobDigest/v1", processed.Digest.NormalisationAlgorithm)
		assert.Equal(t, expected.Encoded(), processed.Digest.Value)

		assert.Nil(t, res.Digest, "input resource must not be mutated")
	})

	t.Run("verifies a matching pre-set digest", func(t *testing.T) {
		res := githubResource(repoURL, testCommit)
		res.Digest = &descriptor.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: "genericBlobDigest/v1",
			Value:                  expected.Encoded(),
		}
		_, err := processor.ProcessResourceDigest(t.Context(), res, nil)
		require.NoError(t, err)
	})

	t.Run("rejects a mismatching pre-set digest", func(t *testing.T) {
		res := githubResource(repoURL, testCommit)
		res.Digest = &descriptor.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: "genericBlobDigest/v1",
			Value:                  "0000000000000000000000000000000000000000000000000000000000000000",
		}
		_, err := processor.ProcessResourceDigest(t.Context(), res, nil)
		assert.ErrorContains(t, err, "mismatch")
	})

	t.Run("pins the resolved commit for a ref-only resource", func(t *testing.T) {
		res := githubResourceRef(repoURL, "main", "")
		processed, err := processor.ProcessResourceDigest(t.Context(), res, nil)
		require.NoError(t, err)

		pinned, ok := processed.Access.(*v1.GitHub)
		require.True(t, ok, "processed access must be typed *v1.GitHub")
		assert.Equal(t, testCommit, pinned.Commit, "commit must be pinned to the resolved sha")
		require.NotNil(t, processed.Digest)
		assert.Equal(t, expected.Encoded(), processed.Digest.Value)

		orig, ok := res.Access.(*v1.GitHub)
		require.True(t, ok)
		assert.Empty(t, orig.Commit, "input resource access must not be mutated")
	})

	t.Run("verifies a matching ref and commit", func(t *testing.T) {
		res := githubResourceRef(repoURL, "main", testCommit)
		processed, err := processor.ProcessResourceDigest(t.Context(), res, nil)
		require.NoError(t, err)
		pinned, ok := processed.Access.(*v1.GitHub)
		require.True(t, ok)
		assert.Equal(t, testCommit, pinned.Commit)
	})

	t.Run("rejects a ref that resolves to a different commit", func(t *testing.T) {
		res := githubResourceRef(repoURL, "main", "1111111111111111111111111111111111111111")
		_, err := processor.ProcessResourceDigest(t.Context(), res, nil)
		assert.ErrorContains(t, err, "mismatch")
	})
}

func TestDigestProcessor_ConsumerIdentity(t *testing.T) {
	identity, err := NewDigestProcessor(nil).GetResourceDigestProcessorCredentialConsumerIdentity(t.Context(),
		githubResource("https://github.com/open-component-model/ocm", testCommit))
	require.NoError(t, err)
	assert.Equal(t, "GitHubRepository", identity[runtime.IdentityAttributeType])
}

func TestDigestProcessor_Scheme(t *testing.T) {
	scheme := NewDigestProcessor(nil).GetResourceRepositoryScheme()
	require.NotNil(t, scheme)
	assert.True(t, scheme.IsRegistered(runtime.NewVersionedType(v1.LegacyType, v1.Version)))
}
